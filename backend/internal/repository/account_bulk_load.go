package repository

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/WilliamWang1721/LightBridge/ent"
	dbaccount "github.com/WilliamWang1721/LightBridge/ent/account"
	dbaccountgroup "github.com/WilliamWang1721/LightBridge/ent/accountgroup"
	dbpredicate "github.com/WilliamWang1721/LightBridge/ent/predicate"
	dbproxy "github.com/WilliamWang1721/LightBridge/ent/proxy"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
	"github.com/WilliamWang1721/LightBridge/internal/service"
	"github.com/lib/pq"
)

func (r *accountRepository) BulkUpdate(ctx context.Context, ids []int64, updates service.AccountBulkUpdate) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}

	setClauses := make([]string, 0, 8)
	args := make([]any, 0, 8)

	idx := 1
	if updates.Name != nil {
		setClauses = append(setClauses, "name = $"+itoa(idx))
		args = append(args, *updates.Name)
		idx++
	}
	if updates.ProxyID != nil {
		// 0 表示清除代理（前端发送 0 而不是 null 来表达清除意图）
		if *updates.ProxyID == 0 {
			setClauses = append(setClauses, "proxy_id = NULL")
		} else {
			setClauses = append(setClauses, "proxy_id = $"+itoa(idx))
			args = append(args, *updates.ProxyID)
			idx++
		}
	}
	if updates.Concurrency != nil {
		setClauses = append(setClauses, "concurrency = $"+itoa(idx))
		args = append(args, *updates.Concurrency)
		idx++
	}
	if updates.Priority != nil {
		setClauses = append(setClauses, "priority = $"+itoa(idx))
		args = append(args, *updates.Priority)
		idx++
	}
	if updates.RateMultiplier != nil {
		setClauses = append(setClauses, "rate_multiplier = $"+itoa(idx))
		args = append(args, *updates.RateMultiplier)
		idx++
	}
	if updates.LoadFactor != nil {
		if *updates.LoadFactor <= 0 {
			setClauses = append(setClauses, "load_factor = NULL")
		} else {
			setClauses = append(setClauses, "load_factor = $"+itoa(idx))
			args = append(args, *updates.LoadFactor)
			idx++
		}
	}
	if updates.Status != nil {
		setClauses = append(setClauses, "status = $"+itoa(idx))
		args = append(args, *updates.Status)
		idx++
	}
	if updates.Schedulable != nil {
		setClauses = append(setClauses, "schedulable = $"+itoa(idx))
		args = append(args, *updates.Schedulable)
		idx++
	}
	// JSONB 需要合并而非覆盖，使用 raw SQL 保持旧行为。
	if len(updates.Credentials) > 0 {
		payload, err := json.Marshal(updates.Credentials)
		if err != nil {
			return 0, err
		}
		setClauses = append(setClauses, "credentials = COALESCE(credentials, '{}'::jsonb) || $"+itoa(idx)+"::jsonb")
		args = append(args, payload)
		idx++
	}
	if len(updates.Extra) > 0 {
		payload, err := json.Marshal(updates.Extra)
		if err != nil {
			return 0, err
		}
		setClauses = append(setClauses, "extra = COALESCE(extra, '{}'::jsonb) || $"+itoa(idx)+"::jsonb")
		args = append(args, payload)
		idx++
	}

	if len(setClauses) == 0 {
		return 0, nil
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := "UPDATE accounts SET " + joinClauses(setClauses, ", ") + " WHERE id = ANY($" + itoa(idx) + ") AND deleted_at IS NULL"
	args = append(args, pq.Array(ids))

	result, err := r.sql.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rows > 0 {
		payload := map[string]any{"account_ids": ids}
		if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountBulkChanged, nil, nil, payload); err != nil {
			logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue bulk update failed: err=%v", err)
		}
		shouldSync := false
		if updates.Status != nil && (*updates.Status == service.StatusError || *updates.Status == service.StatusDisabled) {
			shouldSync = true
		}
		if updates.Schedulable != nil && !*updates.Schedulable {
			shouldSync = true
		}
		if shouldSync {
			r.syncSchedulerAccountSnapshots(ctx, ids)
		}
	}
	return rows, nil
}

type accountGroupQueryOptions struct {
	status      string
	schedulable bool
	platforms   []string // 允许的多个平台，空切片表示不进行平台过滤
}

func (r *accountRepository) queryAccountsByGroup(ctx context.Context, groupID int64, opts accountGroupQueryOptions) ([]service.Account, error) {
	q := r.client.AccountGroup.Query().
		Where(dbaccountgroup.GroupIDEQ(groupID))

	// 通过 account_groups 中间表查询账号，并按需叠加状态/平台/调度能力过滤。
	preds := make([]dbpredicate.Account, 0, 6)
	preds = append(preds, dbaccount.DeletedAtIsNil())
	if opts.status != "" {
		preds = append(preds, dbaccount.StatusEQ(opts.status))
	}
	if len(opts.platforms) > 0 {
		preds = append(preds, dbaccount.PlatformIn(opts.platforms...))
	}
	if opts.schedulable {
		now := time.Now()
		preds = append(preds,
			dbaccount.SchedulableEQ(true),
			tempUnschedulablePredicate(),
			notExpiredPredicate(now),
			dbaccount.Or(dbaccount.OverloadUntilIsNil(), dbaccount.OverloadUntilLTE(now)),
			dbaccount.Or(dbaccount.RateLimitResetAtIsNil(), dbaccount.RateLimitResetAtLTE(now)),
		)
	}

	if len(preds) > 0 {
		q = q.Where(dbaccountgroup.HasAccountWith(preds...))
	}

	groups, err := q.
		Order(
			dbaccountgroup.ByPriority(),
			dbaccountgroup.ByAccountField(dbaccount.FieldPriority),
		).
		WithAccount().
		All(ctx)
	if err != nil {
		return nil, err
	}

	orderedIDs := make([]int64, 0, len(groups))
	accountMap := make(map[int64]*dbent.Account, len(groups))
	for _, ag := range groups {
		if ag.Edges.Account == nil {
			continue
		}
		if _, exists := accountMap[ag.AccountID]; exists {
			continue
		}
		accountMap[ag.AccountID] = ag.Edges.Account
		orderedIDs = append(orderedIDs, ag.AccountID)
	}

	accounts := make([]*dbent.Account, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		if acc, ok := accountMap[id]; ok {
			accounts = append(accounts, acc)
		}
	}

	return r.accountsToService(ctx, accounts)
}

func (r *accountRepository) accountsToService(ctx context.Context, accounts []*dbent.Account) ([]service.Account, error) {
	if len(accounts) == 0 {
		return []service.Account{}, nil
	}

	accountIDs := make([]int64, 0, len(accounts))
	proxyIDs := make([]int64, 0, len(accounts))
	for _, acc := range accounts {
		accountIDs = append(accountIDs, acc.ID)
		if acc.ProxyID != nil {
			proxyIDs = append(proxyIDs, *acc.ProxyID)
		}
	}

	proxyMap, err := r.loadProxies(ctx, proxyIDs)
	if err != nil {
		return nil, err
	}
	groupsByAccount, groupIDsByAccount, accountGroupsByAccount, err := r.loadAccountGroups(ctx, accountIDs)
	if err != nil {
		return nil, err
	}

	outAccounts := make([]service.Account, 0, len(accounts))
	for _, acc := range accounts {
		out := accountEntityToService(acc)
		if out == nil {
			continue
		}
		if acc.ProxyID != nil {
			if proxy, ok := proxyMap[*acc.ProxyID]; ok {
				out.Proxy = proxy
			}
		}
		if groups, ok := groupsByAccount[acc.ID]; ok {
			out.Groups = groups
		}
		if groupIDs, ok := groupIDsByAccount[acc.ID]; ok {
			out.GroupIDs = groupIDs
		}
		if ags, ok := accountGroupsByAccount[acc.ID]; ok {
			out.AccountGroups = ags
		}
		outAccounts = append(outAccounts, *out)
	}

	return outAccounts, nil
}

func tempUnschedulablePredicate() dbpredicate.Account {
	return dbpredicate.Account(func(s *entsql.Selector) {
		col := s.C("temp_unschedulable_until")
		s.Where(entsql.Or(
			entsql.IsNull(col),
			entsql.LTE(col, entsql.Expr("NOW()")),
		))
	})
}

func notExpiredPredicate(now time.Time) dbpredicate.Account {
	return dbaccount.Or(
		dbaccount.ExpiresAtIsNil(),
		dbaccount.ExpiresAtGT(now),
		dbaccount.AutoPauseOnExpiredEQ(false),
	)
}

func (r *accountRepository) loadProxies(ctx context.Context, proxyIDs []int64) (map[int64]*service.Proxy, error) {
	proxyMap := make(map[int64]*service.Proxy)
	if len(proxyIDs) == 0 {
		return proxyMap, nil
	}

	proxies, err := r.client.Proxy.Query().Where(dbproxy.IDIn(proxyIDs...)).All(ctx)
	if err != nil {
		return nil, err
	}

	for _, p := range proxies {
		proxyMap[p.ID] = proxyEntityToService(p)
	}
	return proxyMap, nil
}

func (r *accountRepository) loadAccountGroups(ctx context.Context, accountIDs []int64) (map[int64][]*service.Group, map[int64][]int64, map[int64][]service.AccountGroup, error) {
	groupsByAccount := make(map[int64][]*service.Group)
	groupIDsByAccount := make(map[int64][]int64)
	accountGroupsByAccount := make(map[int64][]service.AccountGroup)

	if len(accountIDs) == 0 {
		return groupsByAccount, groupIDsByAccount, accountGroupsByAccount, nil
	}

	entries, err := r.client.AccountGroup.Query().
		Where(dbaccountgroup.AccountIDIn(accountIDs...)).
		WithGroup().
		Order(dbaccountgroup.ByAccountID(), dbaccountgroup.ByPriority()).
		All(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	for _, ag := range entries {
		groupSvc := groupEntityToService(ag.Edges.Group)
		agSvc := service.AccountGroup{
			AccountID: ag.AccountID,
			GroupID:   ag.GroupID,
			Priority:  ag.Priority,
			CreatedAt: ag.CreatedAt,
			Group:     groupSvc,
		}
		accountGroupsByAccount[ag.AccountID] = append(accountGroupsByAccount[ag.AccountID], agSvc)
		groupIDsByAccount[ag.AccountID] = append(groupIDsByAccount[ag.AccountID], ag.GroupID)
		if groupSvc != nil {
			groupsByAccount[ag.AccountID] = append(groupsByAccount[ag.AccountID], groupSvc)
		}
	}

	return groupsByAccount, groupIDsByAccount, accountGroupsByAccount, nil
}

func (r *accountRepository) loadAccountGroupIDs(ctx context.Context, accountID int64) ([]int64, error) {
	entries, err := r.client.AccountGroup.
		Query().
		Where(dbaccountgroup.AccountIDEQ(accountID)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.GroupID)
	}
	return ids, nil
}

func mergeGroupIDs(a []int64, b []int64) []int64 {
	seen := make(map[int64]struct{}, len(a)+len(b))
	out := make([]int64, 0, len(a)+len(b))
	for _, id := range a {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range b {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func buildSchedulerGroupPayload(groupIDs []int64) map[string]any {
	if len(groupIDs) == 0 {
		return nil
	}
	return map[string]any{"group_ids": groupIDs}
}

func accountEntityToService(m *dbent.Account) *service.Account {
	if m == nil {
		return nil
	}

	rateMultiplier := m.RateMultiplier

	return &service.Account{
		ID:                      m.ID,
		Name:                    m.Name,
		Notes:                   m.Notes,
		Platform:                m.Platform,
		SubPlatform:             m.SubPlatform,
		Type:                    m.Type,
		Credentials:             copyJSONMap(m.Credentials),
		Extra:                   copyJSONMap(m.Extra),
		ProxyID:                 m.ProxyID,
		Concurrency:             m.Concurrency,
		Priority:                m.Priority,
		RateMultiplier:          &rateMultiplier,
		LoadFactor:              m.LoadFactor,
		Status:                  m.Status,
		ErrorMessage:            derefString(m.ErrorMessage),
		LastUsedAt:              m.LastUsedAt,
		ExpiresAt:               m.ExpiresAt,
		AutoPauseOnExpired:      m.AutoPauseOnExpired,
		CreatedAt:               m.CreatedAt,
		UpdatedAt:               m.UpdatedAt,
		Schedulable:             m.Schedulable,
		RateLimitedAt:           m.RateLimitedAt,
		RateLimitResetAt:        m.RateLimitResetAt,
		OverloadUntil:           m.OverloadUntil,
		TempUnschedulableUntil:  m.TempUnschedulableUntil,
		TempUnschedulableReason: derefString(m.TempUnschedulableReason),
		SessionWindowStart:      m.SessionWindowStart,
		SessionWindowEnd:        m.SessionWindowEnd,
		SessionWindowStatus:     derefString(m.SessionWindowStatus),
	}
}

func normalizeJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	return in
}

func copyJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func joinClauses(clauses []string, sep string) string {
	if len(clauses) == 0 {
		return ""
	}
	out := clauses[0]
	for i := 1; i < len(clauses); i++ {
		out += sep + clauses[i]
	}
	return out
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

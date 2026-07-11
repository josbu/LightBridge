package repository

import (
	"context"
	"errors"

	dbent "github.com/WilliamWang1721/LightBridge/ent"
	dbaccount "github.com/WilliamWang1721/LightBridge/ent/account"
	dbaccountgroup "github.com/WilliamWang1721/LightBridge/ent/accountgroup"
	dbgroup "github.com/WilliamWang1721/LightBridge/ent/group"
	"github.com/WilliamWang1721/LightBridge/internal/pkg/logger"
	"github.com/WilliamWang1721/LightBridge/internal/service"
)

// syncSchedulerAccountSnapshot 在账号状态变更时主动同步快照到调度器缓存。
// 当账号被设置为错误、禁用、不可调度或临时不可调度时调用，
// 确保调度器和粘性会话逻辑能及时感知账号的最新状态，避免继续使用不可用账号。
//
// syncSchedulerAccountSnapshot proactively syncs account snapshot to scheduler cache
// when account status changes. Called when account is set to error, disabled,
// unschedulable, or temporarily unschedulable, ensuring scheduler and sticky session
// logic can promptly detect the latest account state and avoid using unavailable accounts.
func (r *accountRepository) syncSchedulerAccountSnapshot(ctx context.Context, accountID int64) {
	if r == nil || r.schedulerCache == nil || accountID <= 0 {
		return
	}
	account, err := r.GetByID(ctx, accountID)
	if err != nil {
		logger.LegacyPrintf("repository.account", "[Scheduler] sync account snapshot read failed: id=%d err=%v", accountID, err)
		return
	}
	if err := r.schedulerCache.SetAccount(ctx, account); err != nil {
		logger.LegacyPrintf("repository.account", "[Scheduler] sync account snapshot write failed: id=%d err=%v", accountID, err)
	}
}

func (r *accountRepository) deleteSchedulerAccountSnapshot(ctx context.Context, accountID int64) {
	if r == nil || r.schedulerCache == nil || accountID <= 0 {
		return
	}
	if err := r.schedulerCache.DeleteAccount(ctx, accountID); err != nil {
		logger.LegacyPrintf("repository.account", "[Scheduler] delete account snapshot failed: id=%d err=%v", accountID, err)
	}
}

func (r *accountRepository) syncSchedulerAccountSnapshots(ctx context.Context, accountIDs []int64) {
	if r == nil || r.schedulerCache == nil || len(accountIDs) == 0 {
		return
	}

	uniqueIDs := make([]int64, 0, len(accountIDs))
	seen := make(map[int64]struct{}, len(accountIDs))
	for _, id := range accountIDs {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	if len(uniqueIDs) == 0 {
		return
	}

	accounts, err := r.GetByIDs(ctx, uniqueIDs)
	if err != nil {
		logger.LegacyPrintf("repository.account", "[Scheduler] batch sync account snapshot read failed: count=%d err=%v", len(uniqueIDs), err)
		return
	}

	for _, account := range accounts {
		if account == nil {
			continue
		}
		if err := r.schedulerCache.SetAccount(ctx, account); err != nil {
			logger.LegacyPrintf("repository.account", "[Scheduler] batch sync account snapshot write failed: id=%d err=%v", account.ID, err)
		}
	}
}

func (r *accountRepository) ClearError(ctx context.Context, id int64) error {
	_, err := r.client.Account.Update().
		Where(dbaccount.IDEQ(id)).
		SetStatus(service.StatusActive).
		SetErrorMessage("").
		Save(ctx)
	if err != nil {
		return err
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue clear error failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) AddToGroup(ctx context.Context, accountID, groupID int64, priority int) error {
	_, err := r.client.AccountGroup.Create().
		SetAccountID(accountID).
		SetGroupID(groupID).
		SetPriority(priority).
		Save(ctx)
	if err != nil {
		return err
	}
	payload := buildSchedulerGroupPayload([]int64{groupID})
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountGroupsChanged, &accountID, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue add to group failed: account=%d group=%d err=%v", accountID, groupID, err)
	}
	return nil
}

func (r *accountRepository) RemoveFromGroup(ctx context.Context, accountID, groupID int64) error {
	_, err := r.client.AccountGroup.Delete().
		Where(
			dbaccountgroup.AccountIDEQ(accountID),
			dbaccountgroup.GroupIDEQ(groupID),
		).
		Exec(ctx)
	if err != nil {
		return err
	}
	payload := buildSchedulerGroupPayload([]int64{groupID})
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountGroupsChanged, &accountID, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue remove from group failed: account=%d group=%d err=%v", accountID, groupID, err)
	}
	return nil
}

func (r *accountRepository) GetGroups(ctx context.Context, accountID int64) ([]service.Group, error) {
	groups, err := r.client.Group.Query().
		Where(
			dbgroup.HasAccountsWith(dbaccount.IDEQ(accountID)),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}

	outGroups := make([]service.Group, 0, len(groups))
	for i := range groups {
		outGroups = append(outGroups, *groupEntityToService(groups[i]))
	}
	return outGroups, nil
}

func (r *accountRepository) BindGroups(ctx context.Context, accountID int64, groupIDs []int64) error {
	existingGroupIDs, err := r.loadAccountGroupIDs(ctx, accountID)
	if err != nil {
		return err
	}
	// 使用事务保证删除旧绑定与创建新绑定的原子性
	tx, err := r.client.Tx(ctx)
	if err != nil && !errors.Is(err, dbent.ErrTxStarted) {
		return err
	}

	var txClient *dbent.Client
	if err == nil {
		defer func() { _ = tx.Rollback() }()
		txClient = tx.Client()
	} else {
		// 已处于外部事务中（ErrTxStarted），复用当前 client
		txClient = r.client
	}

	if _, err := txClient.AccountGroup.Delete().Where(dbaccountgroup.AccountIDEQ(accountID)).Exec(ctx); err != nil {
		return err
	}

	if len(groupIDs) == 0 {
		if tx != nil {
			return tx.Commit()
		}
		return nil
	}

	builders := make([]*dbent.AccountGroupCreate, 0, len(groupIDs))
	for i, groupID := range groupIDs {
		builders = append(builders, txClient.AccountGroup.Create().
			SetAccountID(accountID).
			SetGroupID(groupID).
			SetPriority(i+1),
		)
	}

	if _, err := txClient.AccountGroup.CreateBulk(builders...).Save(ctx); err != nil {
		return err
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	payload := buildSchedulerGroupPayload(mergeGroupIDs(existingGroupIDs, groupIDs))
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountGroupsChanged, &accountID, nil, payload); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue bind groups failed: account=%d err=%v", accountID, err)
	}
	return nil
}

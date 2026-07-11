package repository

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/WilliamWang1721/LightBridge/internal/service"
)

func scanUsageLog(scanner interface{ Scan(...any) error }) (*service.UsageLog, error) {
	var (
		id                    int64
		userID                int64
		apiKeyID              int64
		accountID             int64
		requestID             sql.NullString
		model                 string
		requestedModel        sql.NullString
		upstreamModel         sql.NullString
		groupID               sql.NullInt64
		subscriptionID        sql.NullInt64
		inputTokens           int
		outputTokens          int
		cacheCreationTokens   int
		cacheReadTokens       int
		cacheCreation5m       int
		cacheCreation1h       int
		imageOutputTokens     int
		imageOutputCost       float64
		inputCost             float64
		outputCost            float64
		cacheCreationCost     float64
		cacheReadCost         float64
		totalCost             float64
		actualCost            float64
		rateMultiplier        float64
		accountRateMultiplier sql.NullFloat64
		billingType           int16
		requestTypeRaw        int16
		stream                bool
		openaiWSMode          bool
		durationMs            sql.NullInt64
		firstTokenMs          sql.NullInt64
		userAgent             sql.NullString
		ipAddress             sql.NullString
		imageCount            int
		imageSize             sql.NullString
		imageInputSize        sql.NullString
		imageOutputSize       sql.NullString
		imageSizeSource       sql.NullString
		imageSizeBreakdown    sql.NullString
		serviceTier           sql.NullString
		reasoningEffort       sql.NullString
		inboundEndpoint       sql.NullString
		upstreamEndpoint      sql.NullString
		cacheTTLOverridden    bool
		channelID             sql.NullInt64
		modelMappingChain     sql.NullString
		billingTier           sql.NullString
		billingMode           sql.NullString
		accountStatsCost      sql.NullFloat64
		createdAt             time.Time
	)

	if err := scanner.Scan(
		&id,
		&userID,
		&apiKeyID,
		&accountID,
		&requestID,
		&model,
		&requestedModel,
		&upstreamModel,
		&groupID,
		&subscriptionID,
		&inputTokens,
		&outputTokens,
		&cacheCreationTokens,
		&cacheReadTokens,
		&cacheCreation5m,
		&cacheCreation1h,
		&imageOutputTokens,
		&imageOutputCost,
		&inputCost,
		&outputCost,
		&cacheCreationCost,
		&cacheReadCost,
		&totalCost,
		&actualCost,
		&rateMultiplier,
		&accountRateMultiplier,
		&billingType,
		&requestTypeRaw,
		&stream,
		&openaiWSMode,
		&durationMs,
		&firstTokenMs,
		&userAgent,
		&ipAddress,
		&imageCount,
		&imageSize,
		&imageInputSize,
		&imageOutputSize,
		&imageSizeSource,
		&imageSizeBreakdown,
		&serviceTier,
		&reasoningEffort,
		&inboundEndpoint,
		&upstreamEndpoint,
		&cacheTTLOverridden,
		&channelID,
		&modelMappingChain,
		&billingTier,
		&billingMode,
		&accountStatsCost,
		&createdAt,
	); err != nil {
		return nil, err
	}

	log := &service.UsageLog{
		ID:                    id,
		UserID:                userID,
		APIKeyID:              apiKeyID,
		AccountID:             accountID,
		Model:                 model,
		RequestedModel:        coalesceTrimmedString(requestedModel, model),
		InputTokens:           inputTokens,
		OutputTokens:          outputTokens,
		CacheCreationTokens:   cacheCreationTokens,
		CacheReadTokens:       cacheReadTokens,
		CacheCreation5mTokens: cacheCreation5m,
		CacheCreation1hTokens: cacheCreation1h,
		ImageOutputTokens:     imageOutputTokens,
		ImageOutputCost:       imageOutputCost,
		InputCost:             inputCost,
		OutputCost:            outputCost,
		CacheCreationCost:     cacheCreationCost,
		CacheReadCost:         cacheReadCost,
		TotalCost:             totalCost,
		ActualCost:            actualCost,
		RateMultiplier:        rateMultiplier,
		AccountRateMultiplier: nullFloat64Ptr(accountRateMultiplier),
		BillingType:           int8(billingType),
		RequestType:           service.RequestTypeFromInt16(requestTypeRaw),
		ImageCount:            imageCount,
		CacheTTLOverridden:    cacheTTLOverridden,
		CreatedAt:             createdAt,
	}
	// 先回填 legacy 字段，再基于 legacy + request_type 计算最终请求类型，保证历史数据兼容。
	log.Stream = stream
	log.OpenAIWSMode = openaiWSMode
	log.RequestType = log.EffectiveRequestType()
	log.Stream, log.OpenAIWSMode = service.ApplyLegacyRequestFields(log.RequestType, stream, openaiWSMode)

	if requestID.Valid {
		log.RequestID = requestID.String
	}
	if groupID.Valid {
		value := groupID.Int64
		log.GroupID = &value
	}
	if subscriptionID.Valid {
		value := subscriptionID.Int64
		log.SubscriptionID = &value
	}
	if durationMs.Valid {
		value := int(durationMs.Int64)
		log.DurationMs = &value
	}
	if firstTokenMs.Valid {
		value := int(firstTokenMs.Int64)
		log.FirstTokenMs = &value
	}
	if userAgent.Valid {
		log.UserAgent = &userAgent.String
	}
	if ipAddress.Valid {
		log.IPAddress = &ipAddress.String
	}
	if imageSize.Valid {
		log.ImageSize = &imageSize.String
	}
	if imageInputSize.Valid {
		log.ImageInputSize = &imageInputSize.String
	}
	if imageOutputSize.Valid {
		log.ImageOutputSize = &imageOutputSize.String
	}
	if imageSizeSource.Valid {
		log.ImageSizeSource = &imageSizeSource.String
	}
	log.ImageSizeBreakdown = stringIntMapFromNullJSON(imageSizeBreakdown)
	if serviceTier.Valid {
		log.ServiceTier = &serviceTier.String
	}
	if reasoningEffort.Valid {
		log.ReasoningEffort = &reasoningEffort.String
	}
	if inboundEndpoint.Valid {
		log.InboundEndpoint = &inboundEndpoint.String
	}
	if upstreamEndpoint.Valid {
		log.UpstreamEndpoint = &upstreamEndpoint.String
	}
	if upstreamModel.Valid {
		log.UpstreamModel = &upstreamModel.String
	}
	if channelID.Valid {
		value := channelID.Int64
		log.ChannelID = &value
	}
	if modelMappingChain.Valid {
		log.ModelMappingChain = &modelMappingChain.String
	}
	if billingTier.Valid {
		log.BillingTier = &billingTier.String
	}
	if billingMode.Valid {
		log.BillingMode = &billingMode.String
	}
	if accountStatsCost.Valid {
		log.AccountStatsCost = &accountStatsCost.Float64
	}

	return log, nil
}

func scanTrendRows(rows *sql.Rows) ([]TrendDataPoint, error) {
	results := make([]TrendDataPoint, 0)
	for rows.Next() {
		var row TrendDataPoint
		if err := rows.Scan(
			&row.Date,
			&row.Requests,
			&row.InputTokens,
			&row.OutputTokens,
			&row.CacheCreationTokens,
			&row.CacheReadTokens,
			&row.TotalTokens,
			&row.Cost,
			&row.ActualCost,
		); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func scanModelStatsRows(rows *sql.Rows) ([]ModelStat, error) {
	results := make([]ModelStat, 0)
	for rows.Next() {
		var row ModelStat
		if err := rows.Scan(
			&row.Model,
			&row.Requests,
			&row.InputTokens,
			&row.OutputTokens,
			&row.CacheCreationTokens,
			&row.CacheReadTokens,
			&row.TotalTokens,
			&row.Cost,
			&row.ActualCost,
			&row.AccountCost,
		); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func buildWhere(conditions []string) string {
	if len(conditions) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(conditions, " AND ")
}

func appendRequestTypeOrStreamWhereCondition(conditions []string, args []any, requestType *int16, stream *bool) ([]string, []any) {
	if requestType != nil {
		condition, conditionArgs := buildRequestTypeFilterCondition(len(args)+1, *requestType)
		conditions = append(conditions, condition)
		args = append(args, conditionArgs...)
		return conditions, args
	}
	if stream != nil {
		conditions = append(conditions, fmt.Sprintf("stream = $%d", len(args)+1))
		args = append(args, *stream)
	}
	return conditions, args
}

func appendRequestTypeOrStreamQueryFilter(query string, args []any, requestType *int16, stream *bool) (string, []any) {
	if requestType != nil {
		condition, conditionArgs := buildRequestTypeFilterCondition(len(args)+1, *requestType)
		query += " AND " + condition
		args = append(args, conditionArgs...)
		return query, args
	}
	if stream != nil {
		query += fmt.Sprintf(" AND stream = $%d", len(args)+1)
		args = append(args, *stream)
	}
	return query, args
}

// buildRequestTypeFilterCondition 在 request_type 过滤时兼容 legacy 字段，避免历史数据漏查。
func buildRequestTypeFilterCondition(startArgIndex int, requestType int16) (string, []any) {
	normalized := service.RequestTypeFromInt16(requestType)
	requestTypeArg := int16(normalized)
	switch normalized {
	case service.RequestTypeSync:
		return fmt.Sprintf("(request_type = $%d OR (request_type = %d AND stream = FALSE AND openai_ws_mode = FALSE))", startArgIndex, int16(service.RequestTypeUnknown)), []any{requestTypeArg}
	case service.RequestTypeStream:
		return fmt.Sprintf("(request_type = $%d OR (request_type = %d AND stream = TRUE AND openai_ws_mode = FALSE))", startArgIndex, int16(service.RequestTypeUnknown)), []any{requestTypeArg}
	case service.RequestTypeWSV2:
		return fmt.Sprintf("(request_type = $%d OR (request_type = %d AND openai_ws_mode = TRUE))", startArgIndex, int16(service.RequestTypeUnknown)), []any{requestTypeArg}
	default:
		return fmt.Sprintf("request_type = $%d", startArgIndex), []any{requestTypeArg}
	}
}

func nullInt64(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}

func nullInt(v *int) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
}

func nullFloat64Ptr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	out := v.Float64
	return &out
}

func nullString(v *string) sql.NullString {
	if v == nil || *v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: *v, Valid: true}
}

func nullStringIntMapJSON(v map[string]int) any {
	if len(v) == 0 {
		return nil
	}
	payload, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return string(payload)
}

func stringIntMapFromNullJSON(v sql.NullString) map[string]int {
	if !v.Valid || strings.TrimSpace(v.String) == "" {
		return nil
	}
	var out map[string]int
	if err := json.Unmarshal([]byte(v.String), &out); err != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func coalesceTrimmedString(v sql.NullString, fallback string) string {
	if v.Valid && strings.TrimSpace(v.String) != "" {
		return v.String
	}
	return fallback
}

func setToSlice(set map[int64]struct{}) []int64 {
	out := make([]int64, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}

package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func dropPreviousResponseIDFromRawPayloadWithDeleteFn(
	payload []byte,
	deleteFn func([]byte, string) ([]byte, error),
) ([]byte, bool, error) {
	if len(payload) == 0 {
		return payload, false, nil
	}
	if !gjson.GetBytes(payload, "previous_response_id").Exists() {
		return payload, false, nil
	}
	if deleteFn == nil {
		deleteFn = sjson.DeleteBytes
	}

	updated := payload
	for i := 0; i < openAIWSMaxPrevResponseIDDeletePasses &&
		gjson.GetBytes(updated, "previous_response_id").Exists(); i++ {
		next, err := deleteFn(updated, "previous_response_id")
		if err != nil {
			return payload, false, err
		}
		updated = next
	}
	return updated, !gjson.GetBytes(updated, "previous_response_id").Exists(), nil
}

func setPreviousResponseIDToRawPayload(payload []byte, previousResponseID string) ([]byte, error) {
	normalizedPrevID := strings.TrimSpace(previousResponseID)
	if len(payload) == 0 || normalizedPrevID == "" {
		return payload, nil
	}
	updated, err := sjson.SetBytes(payload, "previous_response_id", normalizedPrevID)
	if err == nil {
		return updated, nil
	}

	var reqBody map[string]any
	if unmarshalErr := json.Unmarshal(payload, &reqBody); unmarshalErr != nil {
		return nil, err
	}
	reqBody["previous_response_id"] = normalizedPrevID
	rebuilt, marshalErr := json.Marshal(reqBody)
	if marshalErr != nil {
		return nil, marshalErr
	}
	return rebuilt, nil
}

func shouldInferIngressFunctionCallOutputPreviousResponseID(
	storeDisabled bool,
	turn int,
	signals ToolContinuationSignals,
	currentPreviousResponseID string,
	expectedPreviousResponseID string,
) bool {
	if !storeDisabled || turn <= 1 || !signals.HasFunctionCallOutput {
		return false
	}
	if strings.TrimSpace(currentPreviousResponseID) != "" {
		return false
	}
	if signals.HasFunctionCallOutputMissingCallID {
		return false
	}
	// If the client already sent the actual tool-call context, treat this as
	// a full replay / self-contained continuation payload rather than
	// downgrading it into an inferred delta continuation. item_reference alone
	// is not enough on the store=false WS path: it still needs a valid prior
	// response anchor so upstream can resolve the referenced function_call.
	if signals.HasToolCallContext {
		return false
	}
	return strings.TrimSpace(expectedPreviousResponseID) != ""
}

func alignStoreDisabledPreviousResponseID(
	payload []byte,
	expectedPreviousResponseID string,
) ([]byte, bool, error) {
	if len(payload) == 0 {
		return payload, false, nil
	}
	expected := strings.TrimSpace(expectedPreviousResponseID)
	if expected == "" {
		return payload, false, nil
	}
	current := openAIWSPayloadStringFromRaw(payload, "previous_response_id")
	if current == "" || current == expected {
		return payload, false, nil
	}

	withoutPrev, removed, dropErr := dropPreviousResponseIDFromRawPayload(payload)
	if dropErr != nil {
		return payload, false, dropErr
	}
	if !removed {
		return payload, false, nil
	}
	updated, setErr := setPreviousResponseIDToRawPayload(withoutPrev, expected)
	if setErr != nil {
		return payload, false, setErr
	}
	return updated, true, nil
}

func cloneOpenAIWSPayloadBytes(payload []byte) []byte {
	if len(payload) == 0 {
		return nil
	}
	cloned := make([]byte, len(payload))
	copy(cloned, payload)
	return cloned
}

func cloneOpenAIWSRawMessages(items []json.RawMessage) []json.RawMessage {
	if items == nil {
		return nil
	}
	cloned := make([]json.RawMessage, 0, len(items))
	for idx := range items {
		cloned = append(cloned, json.RawMessage(cloneOpenAIWSPayloadBytes(items[idx])))
	}
	return cloned
}

func normalizeOpenAIWSJSONForCompare(raw []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("json is empty")
	}
	var decoded any
	if err := json.Unmarshal(trimmed, &decoded); err != nil {
		return nil, err
	}
	return json.Marshal(decoded)
}

func normalizeOpenAIWSJSONForCompareOrRaw(raw []byte) []byte {
	normalized, err := normalizeOpenAIWSJSONForCompare(raw)
	if err != nil {
		return bytes.TrimSpace(raw)
	}
	return normalized
}

func normalizeOpenAIWSPayloadWithoutInputAndPreviousResponseID(payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, errors.New("payload is empty")
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, err
	}
	delete(decoded, "input")
	delete(decoded, "previous_response_id")
	return json.Marshal(decoded)
}

func openAIWSExtractNormalizedInputSequence(payload []byte) ([]json.RawMessage, bool, error) {
	if len(payload) == 0 {
		return nil, false, nil
	}
	inputValue := gjson.GetBytes(payload, "input")
	if !inputValue.Exists() {
		return nil, false, nil
	}
	if inputValue.Type == gjson.JSON {
		raw := strings.TrimSpace(inputValue.Raw)
		if strings.HasPrefix(raw, "[") {
			var items []json.RawMessage
			if err := json.Unmarshal([]byte(raw), &items); err != nil {
				return nil, true, err
			}
			return items, true, nil
		}
		return []json.RawMessage{json.RawMessage(raw)}, true, nil
	}
	if inputValue.Type == gjson.String {
		encoded, _ := json.Marshal(inputValue.String())
		return []json.RawMessage{encoded}, true, nil
	}
	return []json.RawMessage{json.RawMessage(inputValue.Raw)}, true, nil
}

func openAIWSInputIsPrefixExtended(previousPayload, currentPayload []byte) (bool, error) {
	previousItems, previousExists, prevErr := openAIWSExtractNormalizedInputSequence(previousPayload)
	if prevErr != nil {
		return false, prevErr
	}
	currentItems, currentExists, currentErr := openAIWSExtractNormalizedInputSequence(currentPayload)
	if currentErr != nil {
		return false, currentErr
	}
	if !previousExists && !currentExists {
		return true, nil
	}
	if !previousExists {
		return len(currentItems) == 0, nil
	}
	if !currentExists {
		return len(previousItems) == 0, nil
	}
	if len(currentItems) < len(previousItems) {
		return false, nil
	}

	for idx := range previousItems {
		previousNormalized := normalizeOpenAIWSJSONForCompareOrRaw(previousItems[idx])
		currentNormalized := normalizeOpenAIWSJSONForCompareOrRaw(currentItems[idx])
		if !bytes.Equal(previousNormalized, currentNormalized) {
			return false, nil
		}
	}
	return true, nil
}

func openAIWSRawItemsHasPrefix(items []json.RawMessage, prefix []json.RawMessage) bool {
	if len(prefix) == 0 {
		return true
	}
	if len(items) < len(prefix) {
		return false
	}
	for idx := range prefix {
		previousNormalized := normalizeOpenAIWSJSONForCompareOrRaw(prefix[idx])
		currentNormalized := normalizeOpenAIWSJSONForCompareOrRaw(items[idx])
		if !bytes.Equal(previousNormalized, currentNormalized) {
			return false
		}
	}
	return true
}

func openAIWSRawItemsHasFunctionCallOutput(items []json.RawMessage) bool {
	for _, item := range items {
		if isCodexToolCallOutputItemType(gjson.GetBytes(item, "type").String()) {
			return true
		}
	}
	return false
}

func openAIWSRawPayloadHasToolCallOutput(payload []byte) bool {
	if len(payload) == 0 {
		return false
	}
	input := gjson.GetBytes(payload, "input")
	if !input.Exists() {
		return false
	}
	if input.IsArray() {
		for _, item := range input.Array() {
			if isCodexToolCallOutputItemType(item.Get("type").String()) {
				return true
			}
		}
		return false
	}
	if input.Type == gjson.JSON {
		return isCodexToolCallOutputItemType(input.Get("type").String())
	}
	return false
}

func buildOpenAIWSReplayInputSequence(
	previousFullInput []json.RawMessage,
	previousFullInputExists bool,
	currentPayload []byte,
	hasPreviousResponseID bool,
) ([]json.RawMessage, bool, error) {
	currentItems, currentExists, currentErr := openAIWSExtractNormalizedInputSequence(currentPayload)
	if currentErr != nil {
		return nil, false, currentErr
	}
	if !hasPreviousResponseID {
		return cloneOpenAIWSRawMessages(currentItems), currentExists, nil
	}
	if !previousFullInputExists {
		return cloneOpenAIWSRawMessages(currentItems), currentExists, nil
	}
	if !currentExists || len(currentItems) == 0 {
		return cloneOpenAIWSRawMessages(previousFullInput), true, nil
	}
	if openAIWSRawItemsHasPrefix(currentItems, previousFullInput) {
		return cloneOpenAIWSRawMessages(currentItems), true, nil
	}
	merged := make([]json.RawMessage, 0, len(previousFullInput)+len(currentItems))
	merged = append(merged, cloneOpenAIWSRawMessages(previousFullInput)...)
	merged = append(merged, cloneOpenAIWSRawMessages(currentItems)...)
	return merged, true, nil
}

func setOpenAIWSPayloadInputSequence(
	payload []byte,
	fullInput []json.RawMessage,
	fullInputExists bool,
) ([]byte, error) {
	if !fullInputExists {
		return payload, nil
	}
	// Preserve [] vs null semantics when input exists but is empty.
	inputForMarshal := fullInput
	if inputForMarshal == nil {
		inputForMarshal = []json.RawMessage{}
	}
	inputRaw, marshalErr := json.Marshal(inputForMarshal)
	if marshalErr != nil {
		return nil, marshalErr
	}
	return sjson.SetRawBytes(payload, "input", inputRaw)
}

func shouldKeepIngressPreviousResponseID(
	previousPayload []byte,
	currentPayload []byte,
	lastTurnResponseID string,
	hasFunctionCallOutput bool,
) (bool, string, error) {
	if hasFunctionCallOutput {
		return true, "has_function_call_output", nil
	}
	currentPreviousResponseID := strings.TrimSpace(openAIWSPayloadStringFromRaw(currentPayload, "previous_response_id"))
	if currentPreviousResponseID == "" {
		return false, "missing_previous_response_id", nil
	}
	expectedPreviousResponseID := strings.TrimSpace(lastTurnResponseID)
	if expectedPreviousResponseID == "" {
		return false, "missing_last_turn_response_id", nil
	}
	if currentPreviousResponseID != expectedPreviousResponseID {
		return false, "previous_response_id_mismatch", nil
	}
	if len(previousPayload) == 0 {
		return false, "missing_previous_turn_payload", nil
	}

	previousComparable, previousComparableErr := normalizeOpenAIWSPayloadWithoutInputAndPreviousResponseID(previousPayload)
	if previousComparableErr != nil {
		return false, "non_input_compare_error", previousComparableErr
	}
	currentComparable, currentComparableErr := normalizeOpenAIWSPayloadWithoutInputAndPreviousResponseID(currentPayload)
	if currentComparableErr != nil {
		return false, "non_input_compare_error", currentComparableErr
	}
	if !bytes.Equal(previousComparable, currentComparable) {
		return false, "non_input_changed", nil
	}
	return true, "strict_incremental_ok", nil
}

type openAIWSIngressPreviousTurnStrictState struct {
	nonInputComparable []byte
}

func buildOpenAIWSIngressPreviousTurnStrictState(payload []byte) (*openAIWSIngressPreviousTurnStrictState, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	nonInputComparable, nonInputErr := normalizeOpenAIWSPayloadWithoutInputAndPreviousResponseID(payload)
	if nonInputErr != nil {
		return nil, nonInputErr
	}
	return &openAIWSIngressPreviousTurnStrictState{
		nonInputComparable: nonInputComparable,
	}, nil
}

func shouldKeepIngressPreviousResponseIDWithStrictState(
	previousState *openAIWSIngressPreviousTurnStrictState,
	currentPayload []byte,
	lastTurnResponseID string,
	hasFunctionCallOutput bool,
) (bool, string, error) {
	if hasFunctionCallOutput {
		return true, "has_function_call_output", nil
	}
	currentPreviousResponseID := strings.TrimSpace(openAIWSPayloadStringFromRaw(currentPayload, "previous_response_id"))
	if currentPreviousResponseID == "" {
		return false, "missing_previous_response_id", nil
	}
	expectedPreviousResponseID := strings.TrimSpace(lastTurnResponseID)
	if expectedPreviousResponseID == "" {
		return false, "missing_last_turn_response_id", nil
	}
	if currentPreviousResponseID != expectedPreviousResponseID {
		return false, "previous_response_id_mismatch", nil
	}
	if previousState == nil {
		return false, "missing_previous_turn_payload", nil
	}

	currentComparable, currentComparableErr := normalizeOpenAIWSPayloadWithoutInputAndPreviousResponseID(currentPayload)
	if currentComparableErr != nil {
		return false, "non_input_compare_error", currentComparableErr
	}
	if !bytes.Equal(previousState.nonInputComparable, currentComparable) {
		return false, "non_input_changed", nil
	}
	return true, "strict_incremental_ok", nil
}

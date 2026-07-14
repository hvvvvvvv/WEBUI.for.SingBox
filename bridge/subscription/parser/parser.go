package parser

import "strconv"

// Parse converts supported subscription content to sing-box outbounds. Invalid
// or unsupported candidates are skipped; an error is returned only when no
// candidate can be converted.
func Parse(raw string) (Result, error) {
	nodes, inputIssues, total := parseInput(raw)
	result := Result{
		Outbounds: make([]map[string]any, 0, len(nodes)),
		Total:     total,
		Issues:    append([]Issue(nil), inputIssues...),
	}
	result.Skipped = len(result.Issues)
	usedTags := make(map[string]struct{}, len(nodes))
	for _, issue := range inputIssues {
		if issue.Reason == candidateLimitReason {
			return result, &Error{Total: result.Total, Issues: result.Issues}
		}
	}
	for index, node := range nodes {
		outbound, err := produceNode(node)
		if err != nil {
			sourceIndex := index + 1
			if value, ok := nodeInt(node, "_source_index"); ok && value > 0 {
				sourceIndex = value
			}
			result.Issues = append(result.Issues, Issue{
				Index:  sourceIndex,
				Parser: "sing-box producer",
				Reason: err.Error(),
			})
			continue
		}
		outbound["tag"] = uniqueOutboundTag(stringValue(outbound["tag"]), usedTags)
		result.Outbounds = append(result.Outbounds, outbound)
	}
	result.Skipped = len(result.Issues)
	if len(result.Outbounds) == 0 {
		return result, &Error{Total: result.Total, Issues: result.Issues}
	}
	return result, nil
}

func uniqueOutboundTag(base string, used map[string]struct{}) string {
	if _, exists := used[base]; !exists {
		used[base] = struct{}{}
		return base
	}
	for suffix := 2; ; suffix++ {
		candidate := base + " " + strconv.Itoa(suffix)
		if _, exists := used[candidate]; exists {
			continue
		}
		used[candidate] = struct{}{}
		return candidate
	}
}

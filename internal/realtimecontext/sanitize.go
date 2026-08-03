package realtimecontext

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var entityFieldAllowlist = map[string]bool{
	"entityId": true, "name": true, "template": true, "displayTemplate": true, "displayKind": true,
	"specificType": true, "platformType": true, "healthStatus": true, "isLive": true, "isOnline": true,
	"lastMeasurementTime": true, "lastSeenTime": true, "lastStateChangeTime": true, "lastSyncedAt": true,
	"sensorStatus": true, "flightStatus": true, "navigationStatus": true, "areaStatus": true, "ruleStatus": true,
	"workingState": true, "stateMessage": true, "taskStatus": true, "taskMessage": true, "taskType": true,
	"taskPriority": true, "taskProgress": true, "taskStartTime": true, "taskUpdateTime": true,
	"latitude": true, "longitude": true, "geoPoint": true, "altitudeAglMeters": true, "altitudeHaeMeters": true,
	"altitudeMin": true, "altitudeMax": true, "location": true, "pitch": true, "roll": true, "yaw": true,
	"speedMps": true, "maxSpeedMps": true, "maxRangeKm": true, "radius": true, "globalHeight": true,
	"fps": true, "resolutionWidth": true, "resolutionHeight": true, "visibility": true, "hasPtz": true,
	"ptzPan": true, "ptzTilt": true, "ptzZoom": true, "sensorType": true, "sensorHits": true,
	"hasDisposalAbility": true, "radarId": true, "radarModel": true, "radarRange": true, "radarTransmit": true,
	"radarType": true, "trackQuality": true, "dockModeCode": true, "dockSn": true, "droneInDock": true,
	"droneType": true, "autoFlightSpeed": true, "waypointCount": true, "destinationName": true,
	"areaType": true, "alarmLevel": true, "alarmRuleName": true, "alertLevel": true, "filterEnabled": true,
	"priority": true, "ruleType": true, "conditionCount": true, "modelType": true, "modelVersion": true,
	"framework": true, "precision": true, "datasetName": true, "deploymentLocation": true,
}

var safeJSONFields = map[string]bool{
	"statusJson": true, "tasksJson": true, "sensorCapabilitiesJson": true, "radarParametersJson": true,
	"flightParametersJson": true, "navigationParametersJson": true, "routeParametersJson": true,
	"dockWeatherJson": true, "suppliesJson": true, "areaPropertiesJson": true, "metricsJson": true,
	"conditionsJson": true,
}

var ipv4Pattern = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

func sanitizeEntityProperties(properties map[string]any) map[string]any {
	result := make(map[string]any)
	for key, value := range properties {
		if entityFieldAllowlist[key] {
			if clean, ok := sanitizeValue(key, value); ok {
				result[key] = clean
			}
			continue
		}
		if safeJSONFields[key] {
			decoded, ok := decodeJSONObject(value)
			if !ok {
				continue
			}
			if clean, ok := sanitizeValue(key, decoded); ok {
				result[strings.TrimSuffix(key, "Json")] = clean
			}
		}
	}
	return result
}

func decodeJSONObject(value any) (any, bool) {
	switch typed := value.(type) {
	case string:
		var decoded any
		if err := json.Unmarshal([]byte(typed), &decoded); err != nil {
			return nil, false
		}
		return decoded, true
	case map[string]any, []any:
		return typed, true
	default:
		return nil, false
	}
}

func sanitizeValue(key string, value any) (any, bool) {
	if sensitiveKey(key) {
		return nil, false
	}
	switch typed := value.(type) {
	case map[string]any:
		clean := make(map[string]any)
		for childKey, childValue := range typed {
			if child, ok := sanitizeValue(childKey, childValue); ok {
				clean[childKey] = child
			}
		}
		if len(clean) == 0 {
			return nil, false
		}
		return clean, true
	case []any:
		clean := make([]any, 0, len(typed))
		for _, item := range typed {
			if child, ok := sanitizeValue(key, item); ok {
				clean = append(clean, child)
			}
		}
		return clean, true
	case string:
		if ipv4Pattern.MatchString(typed) || strings.Contains(typed, "://") {
			return nil, false
		}
		if len(typed) > 4000 {
			typed = trimUTF8(typed, 4000) + "…"
		}
		return typed, true
	case nil:
		return nil, false
	default:
		return typed, true
	}
}

func sanitizeExternalText(value string) string {
	clean, ok := sanitizeValue("", value)
	if !ok {
		return ""
	}
	text, _ := clean.(string)
	return text
}

func sensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, key))
	for _, token := range []string{
		"password", "passwd", "secret", "token", "credential", "authorization", "auth", "username",
		"apikey", "appkey", "privatekey", "stream", "media", "endpoint", "baseurl", "ipaddress",
		"host", "hostname", "port",
	} {
		if strings.Contains(normalized, token) {
			return true
		}
	}
	return normalized == "ip" || normalized == "url" || normalized == "address"
}

func stringProperty(properties map[string]any, key string) string {
	value, ok := properties[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func boolProperty(properties map[string]any, key string) bool {
	switch value := properties[key].(type) {
	case bool:
		return value
	case float64:
		return value != 0
	case int:
		return value != 0
	case string:
		return strings.EqualFold(value, "true") || value == "1"
	default:
		return false
	}
}

func normalize(value string) string {
	return strings.ToLower(strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, value))
}

func longestCommonCJK(left, right string) int {
	a, b := []rune(left), []rune(right)
	best := 0
	for i := range a {
		if !isCJK(a[i]) {
			continue
		}
		for j := range b {
			if a[i] != b[j] || !isCJK(b[j]) {
				continue
			}
			length := 0
			for i+length < len(a) && j+length < len(b) && a[i+length] == b[j+length] && isCJK(a[i+length]) {
				length++
			}
			best = max(best, length)
		}
	}
	return best
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han)
}

func containsAny(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, normalize(candidate)) {
			return true
		}
	}
	return false
}

func looksLikeEntityID(query string) bool {
	normalized := strings.ToLower(query)
	return regexp.MustCompile(`\b(?:camera|radar|uav|usv|dock|jammer|laser|tdoa)[-_][a-z0-9_-]+\b`).MatchString(normalized)
}

func runeLen(value string) int { return len([]rune(value)) }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

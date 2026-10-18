package option

import (
	"sort"
	"strings"
)

// missingFlags lists the flags (sorted) whose value is empty, comma-separated.
func missingFlags(values map[string]string) string {
	var missing []string
	for name, v := range values {
		if strings.TrimSpace(v) == "" {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return strings.Join(missing, ", ")
}

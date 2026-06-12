package teamtmpl

import "strings"

// Substitution asks the exporter to replace every occurrence of a
// source-workspace literal (a repo URL, a stack name, an owner's name)
// with a {{param.<key>}} placeholder, so the manifest carries the
// parameter, not the source project's value (ADA-30 AC 4).
type Substitution struct {
	Find     string `json:"find"`
	ParamKey string `json:"param_key"`
}

// Substitute rewrites every text field in the manifest, replacing each
// occurrence of sub.Find with the placeholder for sub.ParamKey. Returns
// the total number of replacements. Callers validate that ParamKey is a
// declared parameter (Manifest.Validate enforces it afterwards anyway,
// since the placeholder lands in a linted text location).
func Substitute(m *Manifest, sub Substitution) int {
	if sub.Find == "" {
		return 0
	}
	placeholder := "{{param." + sub.ParamKey + "}}"
	count := 0
	for _, p := range m.textPointers() {
		if n := strings.Count(*p, sub.Find); n > 0 {
			*p = strings.ReplaceAll(*p, sub.Find, placeholder)
			count += n
		}
	}
	return count
}

// textPointers returns mutable references to the same fields
// textLocations reads — keep the two in sync when the schema grows.
func (m *Manifest) textPointers() []*string {
	ptrs := []*string{&m.Description}
	for i := range m.Roles {
		ptrs = append(ptrs, &m.Roles[i].Description, &m.Roles[i].Instructions)
	}
	for i := range m.Skills {
		ptrs = append(ptrs, &m.Skills[i].Description, &m.Skills[i].Content)
		for j := range m.Skills[i].Files {
			ptrs = append(ptrs, &m.Skills[i].Files[j].Content)
		}
	}
	if m.Squad != nil {
		ptrs = append(ptrs, &m.Squad.Description, &m.Squad.Instructions)
	}
	for i := range m.Workflows {
		ptrs = append(ptrs, &m.Workflows[i].Description)
		for j := range m.Workflows[i].Steps {
			ptrs = append(ptrs, &m.Workflows[i].Steps[j].Name)
		}
	}
	for i := range m.Autopilots {
		ptrs = append(ptrs, &m.Autopilots[i].Description, &m.Autopilots[i].IssueTitleTemplate)
	}
	return ptrs
}

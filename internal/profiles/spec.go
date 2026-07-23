// Package profiles implements mcp-recall's declarative TOML compression
// profiles: discovery/loading from user, community, and bundled tiers, the
// three compression strategies, and the resolver that plugs into the handler
// dispatch. Ports src/profiles/*.ts.
package profiles

// StringOrSlice decodes a TOML value that may be a single string or a list of
// strings (mcp_pattern) into a []string.
type StringOrSlice []string

// UnmarshalTOML implements the BurntSushi/toml custom decoder interface.
func (s *StringOrSlice) UnmarshalTOML(v any) error {
	switch x := v.(type) {
	case string:
		*s = []string{x}
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if str, ok := e.(string); ok {
				out = append(out, str)
			}
		}
		*s = out
	}
	return nil
}

// Meta is the [profile] table.
type Meta struct {
	ID          string        `toml:"id"`
	Version     string        `toml:"version"`
	Description string        `toml:"description"`
	MCPPattern  StringOrSlice `toml:"mcp_pattern"`
	ShortName   string        `toml:"short_name"`
	MCPURL      string        `toml:"mcp_url"`
	Author      string        `toml:"author"`
	SampleTool  string        `toml:"sample_tool"`
}

// Strategy is the [strategy] table. Numeric fields are pointers so an unset
// value (nil) falls back to the strategy's default, matching the TS `?? default`.
type Strategy struct {
	Type             string            `toml:"type"`
	ItemsPath        []string          `toml:"items_path"`
	Fields           []string          `toml:"fields"`
	Labels           map[string]string `toml:"labels"`
	MaxItems         *int              `toml:"max_items"`
	MaxCharsPerField *int              `toml:"max_chars_per_field"`
	MaxDepth         *int              `toml:"max_depth"`
	MaxArrayItems    *int              `toml:"max_array_items"`
	MaxChars         *int              `toml:"max_chars"`
	FallbackChars    *int              `toml:"fallback_chars"`
}

// Spec is a parsed profile TOML document.
type Spec struct {
	Profile  Meta         `toml:"profile"`
	Strategy Strategy     `toml:"strategy"`
	Retrain  RetrainHints `toml:"retrain"`
}

// RetrainHints are optional [retrain] hints used by `profiles retrain`.
type RetrainHints struct {
	MaxDepth *int `toml:"max_depth"`
}

// Tier is a profile source tier.
type Tier string

const (
	TierUser      Tier = "user"
	TierCommunity Tier = "community"
	TierBundled   Tier = "bundled"
)

// Loaded is a resolved profile with its patterns and source.
type Loaded struct {
	Spec     Spec
	Tier     Tier
	Patterns []string
	FilePath string
}

// ShortName returns the user-friendly name: explicit short_name, else the id
// with the "mcp__" prefix stripped.
func ShortName(m Meta) string {
	if m.ShortName != "" {
		return m.ShortName
	}
	if len(m.ID) > 5 && m.ID[:5] == "mcp__" {
		return m.ID[5:]
	}
	return m.ID
}

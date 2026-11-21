package crypto_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/crypto"
)

func TestPublicEventCatalogMatchesOpenAPIAndAllSDKs(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..")
	expected := []string{}
	for _, event := range crypto.AllWebhookEventTypes() {
		expected = append(expected, string(event))
	}
	sort.Strings(expected)
	eventPattern := regexp.MustCompile(`(?:invoice|payment|withdrawal|wallet|deposit_address)\.[a-z_]+`)
	for _, path := range []string{"api/openapi/merchant-v1.yaml", "sdk/go/metapus/types.go", "sdk/typescript/src/index.ts", "sdk/python/metapus/events.py"} {
		t.Run(path, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(root, path))
			require.NoError(t, err)
			if strings.HasSuffix(path, ".yaml") {
				start := strings.Index(string(raw), "    EventType:\n")
				require.NotEqual(t, -1, start)
				block := string(raw)[start+len("    EventType:\n"):]
				if end := regexp.MustCompile(`(?m)^    [A-Za-z][A-Za-z0-9_]*:`).FindStringIndex(block); end != nil {
					block = block[:end[0]]
				}
				raw = []byte(block)
			}
			found := map[string]bool{}
			for _, event := range eventPattern.FindAllString(string(raw), -1) {
				found[event] = true
			}
			actual := []string{}
			for event := range found {
				actual = append(actual, event)
			}
			sort.Strings(actual)
			require.Equal(t, expected, actual, "published event catalog drift")
		})
	}
}

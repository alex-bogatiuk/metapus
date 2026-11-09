package outbound_repo

import (
	"strings"
	"testing"
)

func TestNetworkPolicyLockSQLTargetsPolicyRowOnly(t *testing.T) {
	if !strings.Contains(networkPolicyLockSQL, "FOR UPDATE OF p") {
		t.Fatal("lock SQL must use FOR UPDATE OF p so a missing resource policy LEFT JOIN is legal")
	}
	stripped := strings.ReplaceAll(networkPolicyLockSQL, "FOR UPDATE OF p", "")
	if strings.Contains(stripped, "FOR UPDATE") {
		t.Fatal("lock SQL must not lock the nullable LEFT JOIN side")
	}
}

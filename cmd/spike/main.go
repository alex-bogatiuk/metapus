package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	baseURL  = "http://localhost:8080/api/v1"
	tenantID = "5cfe45cb-9035-4e57-93e2-127e960370b8"
)

var token string

func main() {
	fmt.Println("=== Verify Fix: Merchant ref + DeletionMark ===")
	token = doLogin()
	if token == "" {
		fmt.Println("FAIL: login")
		os.Exit(1)
	}

	// 1. Create invoice
	ciBody := map[string]any{
		"organizationId": "d0000000-0000-0000-0000-000000000001",
		"merchantId":     "5cd1b322-1167-444d-a074-03977b704d7e",
		"tokenId":        "b90a8c2b-1c4c-4f91-aea4-fbe59ca3beb4",
		"expectedAmount": "500000",
		"expiresAt":      time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339),
		"date":           time.Now().UTC().Format(time.RFC3339),
		"description":    "Test Merchant Ref",
	}
	_, b := doReq("POST", "/document/crypto-invoice", ciBody)
	var resp map[string]any
	_ = json.Unmarshal([]byte(b), &resp)
	invoiceID := resp["id"].(string)
	fmt.Printf("✓ Created invoice: %s\n", invoiceID)

	// 2. Check merchant in response
	_, b = doReq("GET", "/document/crypto-invoice/"+invoiceID, nil)
	_ = json.Unmarshal([]byte(b), &resp)

	merchant, _ := resp["merchant"].(map[string]any)
	if merchant != nil && merchant["name"] != nil && merchant["name"] != "" {
		fmt.Printf("✅ Merchant resolved: name=%q\n", merchant["name"])
	} else {
		fmt.Printf("❌ Merchant NOT resolved! merchant=%v\n", resp["merchant"])
	}

	// 3. Check merchant in LIST
	_, b = doReq("GET", "/document/crypto-invoice", nil)
	var listResp map[string]any
	_ = json.Unmarshal([]byte(b), &listResp)
	items, _ := listResp["items"].([]any)
	if len(items) > 0 {
		first := items[0].(map[string]any)
		listMerchant, _ := first["merchant"].(map[string]any)
		if listMerchant != nil && listMerchant["name"] != nil {
			fmt.Printf("✅ Merchant in list: name=%q\n", listMerchant["name"])
		} else {
			fmt.Printf("❌ Merchant in list NOT resolved! merchant=%v\n", first["merchant"])
		}
	}

	// 4. Set deletion mark
	_, _ = doReq("POST", "/document/crypto-invoice/"+invoiceID+"/deletion-mark", map[string]any{"marked": true})
	_, b = doReq("GET", "/document/crypto-invoice/"+invoiceID, nil)
	_ = json.Unmarshal([]byte(b), &resp)
	if dm, ok := resp["deletionMark"].(bool); ok && dm {
		fmt.Printf("✅ deletionMark=true in API response\n")
	} else {
		fmt.Printf("❌ deletionMark not set! deletionMark=%v\n", resp["deletionMark"])
	}

	// 5. Show in list with includeDeleted
	_, b = doReq("GET", "/document/crypto-invoice?includeDeleted=true", nil)
	_ = json.Unmarshal([]byte(b), &listResp)
	items, _ = listResp["items"].([]any)
	foundDeleted := false
	for _, item := range items {
		m := item.(map[string]any)
		if m["id"] == invoiceID {
			if dm, ok := m["deletionMark"].(bool); ok && dm {
				foundDeleted = true
			}
		}
	}
	if foundDeleted {
		fmt.Printf("✅ deletionMark=true visible in list\n")
	} else {
		fmt.Printf("❌ Deleted invoice not found in list!\n")
	}

	// Cleanup
	_, _ = doReq("POST", "/document/crypto-invoice/"+invoiceID+"/deletion-mark", map[string]any{"marked": false})
	_, _ = doReq("DELETE", "/document/crypto-invoice/"+invoiceID, nil)
	fmt.Println("\n✓ Cleanup done")
}

func doLogin() string {
	d, _ := json.Marshal(map[string]string{"email": "admin@metapus.io", "password": "Admin123!"})
	req, _ := http.NewRequest("POST", baseURL+"/auth/login", bytes.NewReader(d))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var r map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&r)
	t, _ := r["tokens"].(map[string]any)
	if t == nil { return "" }
	return fmt.Sprintf("%v", t["accessToken"])
}

func doReq(method, path string, body any) (int, string) {
	var br io.Reader
	if body != nil {
		d, _ := json.Marshal(body)
		br = bytes.NewReader(d)
	}
	req, _ := http.NewRequest(method, baseURL+path, br)
	req.Header.Set("X-Tenant-ID", tenantID)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil { return 0, err.Error() }
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(rb)
}

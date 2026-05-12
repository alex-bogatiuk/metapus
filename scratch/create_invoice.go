package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

const tenantID = "5cfe45cb-9035-4e57-93e2-127e960370b8"
const baseURL = "http://localhost:8080/api/v1"

func main() {
	// 1. Get Merchant ID from DB or we know it from seed.
	// Wait, the seed generated "MERCHANT-001" merchant.
	// We can use a query or we can just fetch the merchants list via API.
	
	// Let's login first
	loginPayload := map[string]string{
		"email":    "admin@metapus.io",
		"password": "Admin123!",
	}
	loginBody, _ := json.Marshal(loginPayload)
	req, _ := http.NewRequest("POST", baseURL+"/auth/login", bytes.NewBuffer(loginBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	
	var loginRes map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&loginRes)
	
	var token string
	if tokens, ok := loginRes["tokens"].(map[string]interface{}); ok {
		if at, ok := tokens["accessToken"].(string); ok {
			token = at
		}
	}
	if token == "" {
		panic("No token found")
	}

	// 2. Fetch merchants to get ID of MERCHANT-001
	req, _ = http.NewRequest("GET", baseURL+"/catalog/merchants?limit=10", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Tenant-ID", tenantID)
	resp, err = client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	var merchantsRes map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&merchantsRes)
	items := merchantsRes["items"].([]interface{})
	var merchantID string
	for _, it := range items {
		m := it.(map[string]interface{})
		if m["code"].(string) == "MERCHANT-001" {
			merchantID = m["id"].(string)
			break
		}
	}
	if merchantID == "" {
		panic("Merchant not found")
	}

	// 3. Create API Key
	apiKeyPayload := map[string]interface{}{
		"name": "Test Payment Key",
	}
	apiKeyBody, _ := json.Marshal(apiKeyPayload)
	req, _ = http.NewRequest("POST", baseURL+"/merchant-admin/merchants/"+merchantID+"/api-keys", bytes.NewBuffer(apiKeyBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID)
	resp, err = client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	var apiKeyRes map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&apiKeyRes)
	plaintextKey := apiKeyRes["plaintext"].(string)

	fmt.Println("API Key successfully created!")
	fmt.Println("Key:", plaintextKey)

	// 4. Create Invoice via Merchant API
	invoicePayload := map[string]interface{}{
		"amount":      10500000, // 10.5 USDT
		"currency":    "USDT-TRC20",
		"description": "Test Invoice for API test",
		"orderId":     "TEST-ORD-001",
	}
	invoiceBody, _ := json.Marshal(invoicePayload)
	
	curlCmd := fmt.Sprintf(`curl -X POST http://localhost:8080/merchant/v1/invoices \
  -H "X-Tenant-ID: %s" \
  -H "X-Api-Key: %s" \
  -H "Content-Type: application/json" \
  -d '{"amount": 10500000, "currency": "USDT-TRC20", "description": "Test Invoice for API test", "orderId": "TEST-ORD-001"}'`, tenantID, plaintextKey)

	fmt.Println("\nCurl command to create invoice:")
	fmt.Println(curlCmd)

	req, _ = http.NewRequest("POST", "http://localhost:8080/merchant/v1/invoices", bytes.NewBuffer(invoiceBody))
	req.Header.Set("X-Api-Key", plaintextKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID)
	
	resp, err = client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	
	b, _ := io.ReadAll(resp.Body)
	var invoiceRes map[string]interface{}
	json.Unmarshal(b, &invoiceRes)

	if invoiceRes["id"] == nil {
		fmt.Println("Failed to create invoice:", string(b))
		os.Exit(1)
	}

	invoiceID := invoiceRes["id"].(string)

	fmt.Println("\nInvoice successfully created!")
	fmt.Println("Invoice ID:", invoiceID)
	fmt.Println("Payment Link: http://localhost:3000/pay/" + invoiceID)
}

import json
from uuid import uuid4
from typing import Any
from urllib.error import HTTPError
from urllib.parse import quote
from urllib.request import Request, urlopen


class Client:
    def __init__(self, base_url: str, api_key: str, timeout: float = 10.0) -> None:
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout

    def create_invoice(self, payload: dict[str, Any], idempotency_key: str) -> dict[str, Any]:
        return self._request("POST", "/invoices", payload, {"Idempotency-Key": idempotency_key})

    def get_invoice(self, invoice_id: str) -> dict[str, Any]:
        return self._request("GET", "/invoices/" + quote(invoice_id, safe=""))

    def bind_invoice_reference(
        self, invoice_id: str, idempotency_key: str, *,
        order_id: str | None = None, external_id: str | None = None,
        metadata: dict[str, str] | None = None,
    ) -> dict[str, Any]:
        if not idempotency_key:
            raise ValueError("Idempotency-Key is required for reference binding")
        if order_id is None and external_id is None:
            raise ValueError("order_id or external_id is required")
        payload: dict[str, Any] = {}
        if order_id is not None:
            payload["orderId"] = order_id
        if external_id is not None:
            payload["externalId"] = external_id
        if metadata is not None:
            payload["metadata"] = metadata
        return self._request(
            "PATCH", "/invoices/" + quote(invoice_id, safe="") + "/reference",
            payload, {"Idempotency-Key": idempotency_key},
        )

    def cancel_invoice(self, invoice_id: str) -> None:
        self._request("POST", "/invoices/" + quote(invoice_id, safe="") + "/cancel")

    def list_webhook_endpoints(self) -> dict[str, Any]:
        return self._request("GET", "/webhook-endpoints")

    def create_webhook_endpoint(self, payload: dict[str, Any]) -> dict[str, Any]:
        return self._request("POST", "/webhook-endpoints", payload)

    def get_webhook_endpoint(self, endpoint_id: str) -> dict[str, Any]:
        return self._request("GET", "/webhook-endpoints/" + quote(endpoint_id, safe=""))

    def update_webhook_endpoint(self, endpoint_id: str, payload: dict[str, Any]) -> dict[str, Any]:
        return self._request("PATCH", "/webhook-endpoints/" + quote(endpoint_id, safe=""), payload)

    def delete_webhook_endpoint(self, endpoint_id: str) -> None:
        self._request("DELETE", "/webhook-endpoints/" + quote(endpoint_id, safe=""))

    def rotate_webhook_secret(self, endpoint_id: str) -> dict[str, Any]:
        return self._request("POST", "/webhook-endpoints/" + quote(endpoint_id, safe="") + "/rotate-secret")

    def test_webhook_endpoint(self, endpoint_id: str) -> dict[str, Any]:
        return self._request("POST", "/webhook-endpoints/" + quote(endpoint_id, safe="") + "/test")

    def list_events(self, limit: int = 20, offset: int = 0) -> dict[str, Any]:
        return self._request("GET", f"/events?limit={limit}&offset={offset}")

    def list_event_deliveries(self, event_id: str) -> dict[str, Any]:
        return self._request("GET", "/events/" + quote(event_id, safe="") + "/deliveries")

    def redeliver_event(self, event_id: str, endpoint_id: str) -> dict[str, Any]:
        return self._request("POST", "/events/" + quote(event_id, safe="") + "/redeliver", {"endpointId": endpoint_id})

    def _request(self, method: str, path: str, payload: dict[str, Any] | None = None, headers: dict[str, str] | None = None) -> dict[str, Any]:
        request_headers = {"X-Api-Key": self.api_key, "Content-Type": "application/json"}
        request_headers.update(headers or {})
        if method in {"POST", "PUT", "PATCH", "DELETE"} and "Idempotency-Key" not in request_headers:
            request_headers["Idempotency-Key"] = str(uuid4())
        body = None if payload is None else json.dumps(payload, separators=(",", ":")).encode()
        request = Request(self.base_url + "/merchant/v1" + path, data=body, headers=request_headers, method=method)
        try:
            with urlopen(request, timeout=self.timeout) as response:
                if response.status == 204:
                    return {}
                return json.load(response)
        except HTTPError as error:
            excerpt = error.read(8192).decode("utf-8", errors="replace")
            raise RuntimeError(f"Metapus API returned {error.code}: {excerpt}") from error

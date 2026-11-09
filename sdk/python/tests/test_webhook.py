import hashlib
import hmac
import io
import json
from pathlib import Path
import unittest
from unittest.mock import patch

from metapus import Client, verify_webhook


class WebhookVerifierTest(unittest.TestCase):
    def setUp(self) -> None:
        self.body = (Path(__file__).parents[3] / "api" / "fixtures" / "invoice.completed.json").read_bytes()
        self.timestamp = "1787724723"
        self.secret = "whsec_test"
        signed = self.timestamp.encode() + b"." + self.body
        self.signature = hmac.new(self.secret.encode(), signed, hashlib.sha256).hexdigest()

    def test_dual_signature_and_raw_body(self) -> None:
        event = verify_webhook(
            self.body,
            f"t={self.timestamp},v1={'00' * 32},v1={self.signature}",
            self.secret,
            now=int(self.timestamp),
        )
        self.assertEqual(event["type"], "invoice.completed")

    def test_stale_timestamp_is_rejected(self) -> None:
        with self.assertRaisesRegex(ValueError, "outside tolerance"):
            verify_webhook(self.body, f"t={self.timestamp},v1={self.signature}", self.secret, now=int(self.timestamp) + 301)

    def test_api_client_idempotency_and_cancel(self) -> None:
        class Response:
            def __init__(self, status: int, body: bytes = b"") -> None:
                self.status = status
                self.stream = io.BytesIO(body)

            def read(self, size: int = -1) -> bytes:
                return self.stream.read(size)

            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

        requests = []

        def fake_urlopen(request, timeout):
            requests.append((request, timeout))
            if request.full_url.endswith("/cancel"):
                return Response(204)
            return Response(201, json.dumps({"id": "inv_test", "status": "created"}).encode())

        client = Client("https://api.example", "mk_test")
        with patch("metapus.client.urlopen", side_effect=fake_urlopen):
            client.create_invoice({"amount": "100", "currency": "USDT-TRC20"}, "order-1-create")
            client.cancel_invoice("inv/test")

        self.assertEqual(requests[0][0].get_header("Idempotency-key"), "order-1-create")
        self.assertEqual(requests[0][0].get_header("X-api-key"), "mk_test")
        self.assertTrue(requests[1][0].full_url.endswith("/invoices/inv%2Ftest/cancel"))

    def test_api_client_webhook_endpoint_crud_and_test(self) -> None:
        class Response:
            def __init__(self, status: int, body: bytes = b"") -> None:
                self.status = status
                self.stream = io.BytesIO(body)

            def read(self, size: int = -1) -> bytes:
                return self.stream.read(size)

            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

        requests = []

        def fake_urlopen(request, timeout):
            requests.append(request)
            if request.get_method() == "DELETE":
                return Response(204)
            if request.full_url.endswith("/test"):
                return Response(200, b'{"eventId":"evt_test","success":true}')
            return Response(200, b'{"id":"ep_test","object":"webhookEndpoint"}')

        client = Client("https://api.example", "mk_test")
        with patch("metapus.client.urlopen", side_effect=fake_urlopen):
            client.get_webhook_endpoint("ep/test")
            client.update_webhook_endpoint("ep/test", {"version": 1, "isActive": False})
            result = client.test_webhook_endpoint("ep/test")
            client.delete_webhook_endpoint("ep/test")

        self.assertTrue(result["success"])
        self.assertEqual([request.get_method() for request in requests], ["GET", "PATCH", "POST", "DELETE"])
        self.assertTrue(all("/webhook-endpoints/ep%2Ftest" in request.full_url for request in requests))


if __name__ == "__main__":
    unittest.main()

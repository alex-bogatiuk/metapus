import unittest
from unittest.mock import patch

from metapus.client import Client


class ReferenceBindingTest(unittest.TestCase):
    def test_external_only_and_metadata_presence(self):
        client = Client("https://merchant.example", "mk_test")
        with patch.object(client, "_request", return_value={"externalId": "1C-entry-1"}) as request:
            for metadata in (None, {}, {"erp": "1c"}):
                result = client.bind_invoice_reference(
                    "inv/1", "reference-retry", external_id="1C-entry-1", metadata=metadata,
                )
                self.assertEqual(result["externalId"], "1C-entry-1")
                args = request.call_args.args
                self.assertEqual(args[:2], ("PATCH", "/invoices/inv%2F1/reference"))
                self.assertNotIn("orderId", args[2])
                self.assertEqual(args[2]["externalId"], "1C-entry-1")
                self.assertEqual("metadata" in args[2], metadata is not None)
                self.assertEqual(args[3], {"Idempotency-Key": "reference-retry"})

    def test_requires_explicit_retry_key_and_reference(self):
        client = Client("https://merchant.example", "mk_test")
        with self.assertRaises(ValueError):
            client.bind_invoice_reference("inv/1", "", external_id="ERP-1")
        with self.assertRaises(ValueError):
            client.bind_invoice_reference("inv/1", "reference-retry")

# Provisioning-only proxy. Its tenant-scoped AppRole has no signing capability.
vault {
  address = "https://vault-test:8200"
  ca_cert = "/vault-proxy/ca/ca.crt"
  retry { num_retries = 5 }
}
auto_auth {
  method {
    type = "approle"
    mount_path = "auth/approle"
    config = {
      role_id_file_path = "/run/metapus/vault-role/role-id"
      secret_id_file_path = "/run/metapus/vault-bootstrap/wrapped-secret-id"
      remove_secret_id_file_after_reading = true
      secret_id_response_wrapping_path = "auth/approle/role/metapus-aml-control/secret-id"
    }
  }
}
api_proxy { use_auto_auth_token = "force" }
listener "tcp" {
  address = "172.30.252.2:8100"
  tls_disable = true
}

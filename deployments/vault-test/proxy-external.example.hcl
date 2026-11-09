# Copy outside the repository, set the external Vault address/CA, and mount the
# resulting file as VAULT_PROXY_CONFIG_FILE. Application settings do not change.
vault {
  address = "https://vault.example.internal:8200"
  ca_cert = "/vault-proxy/ca/ca.crt"
  retry { num_retries = 5 }
}

auto_auth {
  method {
    type       = "approle"
    mount_path = "auth/approle"
    config = {
      role_id_file_path                   = "/run/metapus/vault-role/role-id"
      secret_id_file_path                 = "/run/metapus/vault-bootstrap/wrapped-secret-id"
      remove_secret_id_file_after_reading = true
      secret_id_response_wrapping_path    = "auth/approle/role/metapus-worker/secret-id"
    }
  }
}

api_proxy { use_auto_auth_token = "force" }
listener "tcp" {
  address     = "0.0.0.0:8100"
  tls_disable = true
}

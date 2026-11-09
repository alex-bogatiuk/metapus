ui            = false
disable_mlock = true
api_addr      = "https://vault-test:8200"
cluster_addr  = "https://vault-test:8201"
plugin_directory = "/vault/plugins"

listener "tcp" {
  address         = "0.0.0.0:8200"
  cluster_address = "0.0.0.0:8201"
  tls_disable     = false
  tls_cert_file   = "/vault/tls/server.crt"
  tls_key_file    = "/vault/tls/server.key"
}

storage "raft" {
  path    = "/vault/data"
  node_id = "metapus-vault-test-1"
}

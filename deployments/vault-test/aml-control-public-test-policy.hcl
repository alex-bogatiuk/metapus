# Public-test tenant only. No signing or key export. Network parameters are pinned.
path "auth/token/renew-self" { capabilities = ["update"] }
path "tron/tenants/01a0cfbe-5e01-72f5-8484-2046b6a672a5/config/readiness" { capabilities = ["read"] }
path "tron/tenants/01a0cfbe-5e01-72f5-8484-2046b6a672a5/wallets/derive" {
  capabilities = ["update"]
  required_parameters = ["derivation_path", "network"]
  allowed_parameters = {
    "network" = ["tron_nile"]
    "derivation_path" = ["m/44'/195'/496144410'/0/*"]
  }
}
path "tron/tenants/01a0cfbe-5e01-72f5-8484-2046b6a672a5/wallets/custody" {
  capabilities = ["update"]
  required_parameters = ["network", "address", "merchant_id", "role"]
  allowed_parameters = {
    "network" = ["tron_nile"]
    "address" = []
    "merchant_id" = ["01a0d418-e47a-752d-98df-d5fac5acb955"]
    "role" = ["merchant_pool", "liquidity_hot", "network_fee"]
  }
}
path "evm/tenants/01a0cfbe-5e01-72f5-8484-2046b6a672a5/config/readiness" { capabilities = ["read"] }
path "evm/tenants/01a0cfbe-5e01-72f5-8484-2046b6a672a5/wallets/derive" {
  capabilities = ["update"]
  required_parameters = ["derivation_path", "chain_id"]
  allowed_parameters = {
    "chain_id" = ["11155111"]
    "derivation_path" = ["m/44'/60'/496144410'/0/*"]
  }
}
path "evm/tenants/01a0cfbe-5e01-72f5-8484-2046b6a672a5/wallets/custody" {
  capabilities = ["update"]
  required_parameters = ["network", "address", "merchant_id", "role"]
  allowed_parameters = {
    "network" = ["11155111"]
    "address" = []
    "merchant_id" = ["01a0d418-e47a-752d-98df-d5fac5acb955"]
    "role" = ["merchant_pool", "liquidity_hot", "network_fee"]
  }
}

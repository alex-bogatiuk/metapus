# Internal-transaction classification v2

Verified against java-tron commit b33eed89a6a424c498d4fb1b03ca2c86eddf4840 (20 October 2026):

- [Program](https://github.com/tronprotocol/java-tron/blob/b33eed89a6a424c498d4fb1b03ca2c86eddf4840/actuator/src/main/java/org/tron/core/vm/program/Program.java).
- Native processors under the same commit: FreezeBalanceProcessor, UnfreezeBalanceProcessor, DelegateResourceProcessor and UnDelegateResourceProcessor.

Mandatory asset movements: call/create/suicide and withdrawExpireUnfreezeWhileSuiciding. The last case credits the inheritor's liquid balance and must remain part of the proof.

Excluded only after validating the exact note and receiver/value shape:

- voteWitness (no receiver or native value).
- freezeBalanceV2ForEnergy/Bandwidth/TronPower, unfreezeBalanceV2ForEnergy/Bandwidth/TronPower, cancelAllUnfreezeV2, withdrawReward, withdrawExpireUnfreeze, withdrawExpireUnfreezeWhileUnfreezing, withdrawExpireUnfreezeWhileCanceling: caller-owned resources/allowance; receiver must be caller. A release/reward on the caller's own balance is not an inbound merchant transfer.
- freezeForEnergy/Bandwidth, unfreezeForEnergy/Bandwidth, delegateResourceOfEnergy/Bandwidth, unDelegateResourceOfEnergy/Bandwidth: receiver identifies a resource beneficiary. Frozen/delegated balances are adjusted; unfreeze returns liquid funds to the owner, not to this beneficiary.

Zero, rejected and TRC10 entries of mandatory operations remain in canonical evidence and participate in scalar index enumeration before payment filtering. Unsupported operation names or invalid shapes halt verification; they cannot be bypassed by selecting another source.

This policy covers the audited operation set. It is not a blanket removal of all featured transactions, and it does not replace treasury/resource accounting for the caller's own staking/reward operations.

## TronPower correction — 22 October 2026

The same audited Program generates `freezeBalanceV2ForTronPower` and
`unfreezeBalanceV2ForTronPower`: `convertResourceToString(2)` returns `TronPower`.
Both traces are created with owner as caller and receiver before processor
validation, so a failed attempt remains in the receipt with `rejected=true`.
An outer contract can handle that failure and still return SUCCESS.

- [FreezeBalanceV2Processor](https://github.com/tronprotocol/java-tron/blob/b33eed89a6a424c498d4fb1b03ca2c86eddf4840/actuator/src/main/java/org/tron/core/vm/nativecontract/FreezeBalanceV2Processor.java)
  validates TRON_POWER against the voting feature flag, moves the owner's liquid
  balance into the owner's frozen TronPower balance, and credits no counterparty.
- [UnfreezeBalanceV2Processor](https://github.com/tronprotocol/java-tron/blob/b33eed89a6a424c498d4fb1b03ca2c86eddf4840/actuator/src/main/java/org/tron/core/vm/nativecontract/UnfreezeBalanceV2Processor.java)
  reduces the owner's frozen TronPower balance and schedules the owner's release.
  Expired releases are credited to that same owner's balance.

Both exact names use the existing caller-resource shape checks: valid caller,
receiver equals caller, exactly one nonnegative native value and no token ID.
Rejected traces still undergo those checks. UnknownType, unknown names and
malformed shapes remain semantic failures; no prefix matching or blanket
rejected-trace bypass is permitted.

Regression fixtures `nile-72026431-*` were read from public TronGrid FullNode and
Solidity APIs on 22 October 2026. The block has eight receipts; transaction
`fe19adf1485422c34283ce14d273098674c190ad83da21c3f1bfe05c4bd08d75` contains a
rejected `freezeBalanceV2ForTronPower` attempt for 1,000,000 SUN. Tests compare
its payment projection with a provider omitting the resource trace while
requiring different source digests and a typed exclusion count.

Decoder version remains 2: these names previously failed normalization and
produced no accepted v2 proof. Previously accepted payloads retain exactly the
same projection, digests and event identities. Existing proofs are not rewritten;
all runtime consumers must use the corrected release.

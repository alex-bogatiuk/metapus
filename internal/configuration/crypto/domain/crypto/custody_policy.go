package crypto

import "metapus/pkg/sdk/id"

func SameCustodyOwner(a, b *id.ID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b && !id.IsNil(*a)
}
func CustodyOwnerMatchesMerchant(owner *id.ID, merchant id.ID, enforced bool) bool {
	if owner == nil {
		return !enforced
	}
	return !id.IsNil(merchant) && *owner == merchant
}

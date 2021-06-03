package accounts

import (
	"pandora-pay/blockchain/accounts/account"
	"pandora-pay/cryptography"
	"pandora-pay/helpers"
	"pandora-pay/store/hash-map"
	store_db_interface "pandora-pay/store/store-db/store-db-interface"
)

type Accounts struct {
	hash_map.HashMap `json:"-"`
}

func NewAccounts(tx store_db_interface.StoreDBTransactionInterface) *Accounts {
	return &Accounts{
		HashMap: *hash_map.CreateNewHashMap(tx, "Accounts", cryptography.PublicKeyHashHashSize),
	}
}

func (accounts *Accounts) GetAccountEvenEmpty(key []byte, chainHeight uint64) (acc *account.Account, err error) {

	acc = new(account.Account)

	data := accounts.Get(string(key))
	if data == nil {
		return
	}

	if err = acc.Deserialize(helpers.NewBufferReader(data)); err != nil {
		return
	}

	if err = acc.RefreshDelegatedStake(chainHeight); err != nil {
		return
	}

	return
}

func (accounts *Accounts) GetAccount(key []byte, chainHeight uint64) (acc *account.Account, err error) {

	data := accounts.Get(string(key))
	if data == nil {
		return
	}

	acc = new(account.Account)
	if err = acc.Deserialize(helpers.NewBufferReader(data)); err != nil {
		return
	}

	if err = acc.RefreshDelegatedStake(chainHeight); err != nil {
		return
	}

	return
}

func (accounts *Accounts) UpdateAccount(key []byte, acc *account.Account) {
	if acc.IsAccountEmpty() {
		accounts.Delete(string(key))
		return
	}
	accounts.Update(string(key), acc.SerializeToBytes())
}

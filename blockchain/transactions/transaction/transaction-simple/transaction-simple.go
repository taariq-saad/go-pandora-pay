package transaction_simple

import (
	"errors"
	"fmt"
	"pandora-pay/blockchain/data/accounts"
	plain_accounts "pandora-pay/blockchain/data/plain-accounts"
	plain_account "pandora-pay/blockchain/data/plain-accounts/plain-account"
	"pandora-pay/blockchain/data/registrations"
	"pandora-pay/blockchain/data/tokens"
	transaction_base_interface "pandora-pay/blockchain/transactions/transaction/transaction-base-interface"
	transaction_data "pandora-pay/blockchain/transactions/transaction/transaction-data"
	"pandora-pay/blockchain/transactions/transaction/transaction-simple/transaction-simple-extra"
	"pandora-pay/blockchain/transactions/transaction/transaction-simple/transaction-simple-parts"
	"pandora-pay/config"
	"pandora-pay/cryptography/crypto"
	"pandora-pay/helpers"
)

type TransactionSimple struct {
	transaction_base_interface.TransactionBaseInterface
	transaction_simple_extra.TransactionSimpleExtraInterface
	TxScript    ScriptType
	DataVersion transaction_data.TransactionDataVersion
	Data        []byte
	Nonce       uint64
	Fee         uint64
	Vin         *transaction_simple_parts.TransactionSimpleInput
	Bloom       *TransactionSimpleBloom
}

func (tx *TransactionSimple) IncludeTransaction(blockHeight uint64, regs *registrations.Registrations, plainAccs *plain_accounts.PlainAccounts, accsCollection *accounts.AccountsCollection, toks *tokens.Tokens) (err error) {

	var plainAcc *plain_account.PlainAccount
	if plainAcc, err = plainAccs.GetPlainAccount(tx.Vin.PublicKey, blockHeight); err != nil {
		return
	}
	if plainAcc == nil {
		return errors.New("Plain Account was not found")
	}

	if plainAcc.Nonce != tx.Nonce {
		return fmt.Errorf("Account nonce doesn't match %d %d", plainAcc.Nonce, tx.Nonce)
	}
	if err = plainAcc.IncrementNonce(true); err != nil {
		return
	}

	switch tx.TxScript {
	case SCRIPT_UPDATE_DELEGATE, SCRIPT_UNSTAKE:
		err = plainAcc.DelegatedStake.AddStakeAvailable(false, tx.Fee)
	case SCRIPT_CLAIM:
		err = plainAcc.AddClaimable(false, tx.Fee)
	default:
		err = errors.New("Invalid TxScript")
	}

	if err != nil {
		return
	}

	switch tx.TxScript {
	case SCRIPT_UPDATE_DELEGATE, SCRIPT_UNSTAKE, SCRIPT_CLAIM:
		if err = tx.TransactionSimpleExtraInterface.IncludeTransactionVin0(blockHeight, plainAcc, regs, plainAccs, accsCollection, toks); err != nil {
			return
		}
	}

	if err = plainAccs.Update(string(tx.Vin.PublicKey), plainAcc); err != nil {
		return
	}

	return
}

func (tx *TransactionSimple) ComputeFees() uint64 {
	return tx.Fee
}

func (tx *TransactionSimple) ComputeAllKeys(out map[string]bool) {
	out[string(tx.Vin.PublicKey)] = true
	return
}

func (tx *TransactionSimple) VerifySignatureManually(hashForSignature []byte) bool {
	if crypto.VerifySignature(hashForSignature, tx.Vin.Signature, tx.Vin.PublicKey) == false {
		return false
	}
	return true
}

func (tx *TransactionSimple) Validate() (err error) {

	if err = tx.Vin.Validate(); err != nil {
		return
	}

	switch tx.TxScript {
	case SCRIPT_UPDATE_DELEGATE, SCRIPT_UNSTAKE:
		if tx.TransactionSimpleExtraInterface == nil {
			return errors.New("extra is not assigned")
		}
		if err = tx.TransactionSimpleExtraInterface.Validate(); err != nil {
			return
		}
	default:
		return errors.New("Invalid TxScript")
	}

	return
}

func (tx *TransactionSimple) SerializeAdvanced(w *helpers.BufferWriter, inclSignature bool) {

	w.WriteUvarint(uint64(tx.TxScript))

	w.WriteByte(byte(tx.DataVersion))
	if tx.DataVersion != transaction_data.TX_DATA_NONE {
		w.WriteUvarint(uint64(len(tx.Data)))
		w.Write(tx.Data)
	}

	w.WriteUvarint(tx.Nonce)
	w.WriteUvarint(tx.Fee)

	tx.Vin.Serialize(w, inclSignature)

	if tx.TransactionSimpleExtraInterface != nil {
		tx.TransactionSimpleExtraInterface.Serialize(w)
	}
}

func (tx *TransactionSimple) Serialize(w *helpers.BufferWriter) {
	tx.SerializeAdvanced(w, true)
}

func (tx *TransactionSimple) SerializeToBytes() []byte {
	w := helpers.NewBufferWriter()
	tx.Serialize(w)
	return w.Bytes()
}

func (tx *TransactionSimple) Deserialize(r *helpers.BufferReader) (err error) {

	var n uint64

	if n, err = r.ReadUvarint(); err != nil {
		return
	}

	scriptType := ScriptType(n)
	if scriptType >= SCRIPT_END {
		return errors.New("INVALID SCRIPT TYPE")
	}

	tx.TxScript = scriptType
	switch tx.TxScript {
	case SCRIPT_UNSTAKE:
		tx.TransactionSimpleExtraInterface = &transaction_simple_extra.TransactionSimpleUnstake{}
	case SCRIPT_UPDATE_DELEGATE:
		tx.TransactionSimpleExtraInterface = &transaction_simple_extra.TransactionSimpleUpdateDelegate{}
	default:
		return errors.New("Invalid TxType")
	}

	var dataVersion byte
	if dataVersion, err = r.ReadByte(); err != nil {
		return
	}

	tx.DataVersion = transaction_data.TransactionDataVersion(dataVersion)
	switch tx.DataVersion {
	case transaction_data.TX_DATA_NONE:
	case transaction_data.TX_DATA_PLAIN_TEXT, transaction_data.TX_DATA_ENCRYPTED:
		if n, err = r.ReadUvarint(); err != nil {
			return
		}
		if n == 0 || n > config.TRANSACTIONS_MAX_DATA_LENGTH {
			return errors.New("Tx.Data length is invalid")
		}
		if tx.Data, err = r.ReadBytes(int(n)); err != nil {
			return
		}
	default:
		return errors.New("Invalid Tx.DataVersion")
	}

	if tx.Nonce, err = r.ReadUvarint(); err != nil {
		return
	}

	if tx.Fee, err = r.ReadUvarint(); err != nil {
		return
	}

	tx.Vin = &transaction_simple_parts.TransactionSimpleInput{}
	if err = tx.Vin.Deserialize(r); err != nil {
		return
	}

	if tx.TransactionSimpleExtraInterface != nil {
		return tx.TransactionSimpleExtraInterface.Deserialize(r)
	}

	return
}

func (tx *TransactionSimple) VerifyBloomAll() (err error) {
	return tx.Bloom.verifyIfBloomed()
}

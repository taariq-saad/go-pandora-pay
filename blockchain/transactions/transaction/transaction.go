package transaction

import (
	"errors"
	transaction_simple "pandora-pay/blockchain/transactions/transaction/transaction-simple"
	transaction_type "pandora-pay/blockchain/transactions/transaction/transaction-type"
	"pandora-pay/crypto"
	"pandora-pay/helpers"
)

type Transaction struct {
	Version uint64
	TxType  transaction_type.TransactionType
	TxBase  interface{}
}

func (tx *Transaction) SerializeForSigning() helpers.Hash {
	return crypto.SHA3Hash(tx.Serialize(false))
}

func (tx *Transaction) VerifySignature() bool {
	hash := tx.SerializeForSigning()
	if tx.IsTransactionSimple() {
		base := tx.TxBase.(transaction_simple.TransactionSimple)
		return base.VerifySignature(hash)
	} else {
		//not implemented
	}
	return false
}

func (tx *Transaction) ComputeHash() helpers.Hash {
	return crypto.SHA3Hash(tx.Serialize(true))
}

func (tx *Transaction) Serialize(inclSignature bool) []byte {
	writer := helpers.NewBufferWriter()

	writer.WriteUint64(tx.Version)
	writer.WriteUint64(uint64(tx.TxType))

	if tx.IsTransactionSimple() {
		base := tx.TxBase.(transaction_simple.TransactionSimple)
		base.Serialize(writer, inclSignature, tx.TxType)
	}

	return writer.Bytes()
}

func (tx *Transaction) Deserialize(buf []byte) (err error) {
	reader := helpers.NewBufferReader(buf)

	if tx.Version, err = reader.ReadUvarint(); err != nil {
		return
	}
	if tx.Version != 0 {
		err = errors.New("Version is invalid")
		return
	}

	var n uint64
	if n, err = reader.ReadUvarint(); err != nil {
		return
	}
	tx.TxType = transaction_type.TransactionType(n)
	if tx.IsTransactionSimple() {

		base := new(transaction_simple.TransactionSimple)
		if err = base.Deserialize(reader, tx.TxType); err != nil {
			return err
		}
		tx.TxBase = base

	} else {
		err = errors.New("Transaction Type is invalid")
		return
	}

	return
}

func (tx *Transaction) IsTransactionSimple() bool {
	return tx.TxType == transaction_type.TransactionTypeSimple || tx.TxType == transaction_type.TransactionTypeSimpleUnstake
}

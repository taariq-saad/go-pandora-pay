package transaction_zether_payload_extra

import (
	"bytes"
	"errors"
	"pandora-pay/blockchain/data_storage"
	"pandora-pay/blockchain/data_storage/assets/asset"
	"pandora-pay/blockchain/transactions/transaction/transaction_zether/transaction_zether_registrations"
	"pandora-pay/config/config_coins"
	"pandora-pay/cryptography"
	"pandora-pay/cryptography/crypto"
	"pandora-pay/helpers"
)

type TransactionZetherPayloadExtraAssetCreate struct {
	TransactionZetherPayloadExtraInterface
	Asset *asset.Asset
}

func (payloadExtra *TransactionZetherPayloadExtraAssetCreate) BeforeIncludeTxPayload(txRegistrations *transaction_zether_registrations.TransactionZetherDataRegistrations, payloadIndex byte, payloadAsset []byte, payloadBurnValue uint64, payloadStatement *crypto.Statement, publicKeyList [][]byte, blockHeight uint64, dataStorage *data_storage.DataStorage) (err error) {
	return
}

func (payloadExtra *TransactionZetherPayloadExtraAssetCreate) IncludeTxPayload(txRegistrations *transaction_zether_registrations.TransactionZetherDataRegistrations, payloadIndex byte, payloadAsset []byte, payloadBurnValue uint64, payloadStatement *crypto.Statement, publicKeyList [][]byte, blockHeight uint64, dataStorage *data_storage.DataStorage) (err error) {

	list := helpers.NewBufferWriter()
	list.WriteByte(payloadIndex)
	list.WriteUvarint(blockHeight)
	for _, publicKey := range publicKeyList {
		list.Write(publicKey)
	}

	hash := cryptography.RIPEMD(cryptography.SHA3(list.Bytes()))
	if err = dataStorage.Asts.CreateAsset(hash, payloadExtra.Asset); err != nil {
		return
	}

	return
}

func (payloadExtra *TransactionZetherPayloadExtraAssetCreate) Validate(txRegistrations *transaction_zether_registrations.TransactionZetherDataRegistrations, payloadIndex byte, payloadAsset []byte, payloadBurnValue uint64, payloadStatement *crypto.Statement) error {

	if payloadExtra.Asset.Supply != 0 {
		return errors.New("AssetInfo Supply must be zero")
	}
	if !bytes.Equal(payloadAsset, config_coins.NATIVE_ASSET_FULL) {
		return errors.New("payloadAsset must be NATIVE_ASSET_FULL")
	}

	return payloadExtra.Asset.Validate()
}

func (payloadExtra *TransactionZetherPayloadExtraAssetCreate) Serialize(w *helpers.BufferWriter, inclSignature bool) {
	payloadExtra.Asset.Serialize(w)
}

func (payloadExtra *TransactionZetherPayloadExtraAssetCreate) Deserialize(r *helpers.BufferReader) (err error) {
	return payloadExtra.Asset.Deserialize(r)
}

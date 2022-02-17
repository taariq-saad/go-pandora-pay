//go:build !wasm
// +build !wasm

package api_faucet

import (
	"context"
	"errors"
	"go.jolheiser.com/hcaptcha"
	"net/http"
	"pandora-pay/config"
	"pandora-pay/config/config_coins"
	"pandora-pay/txs_builder"
	"pandora-pay/txs_builder/wizard"
)

func (api *Faucet) GetFaucetCoins(r *http.Request, args *APIFaucetCoinsRequest, reply *APIFaucetCoinsReply) error {

	if !config.FAUCET_TESTNET_ENABLED {
		return errors.New("Faucet Testnet is not enabled")
	}

	resp, err := api.hcpatchaClient.Verify(args.FaucetToken, hcaptcha.PostOptions{})
	if err != nil {
		return err
	}

	if !resp.Success {
		return errors.New("Faucet token is invalid")
	}

	addr, err := api.wallet.GetWalletAddress(1, true)
	if err != nil {
		return err
	}

	txData := &txs_builder.TxBuilderCreateZetherTxData{
		Payloads: []*txs_builder.TxBuilderCreateZetherTxPayload{{
			Sender:            addr.AddressEncoded,
			Asset:             config_coins.NATIVE_ASSET_FULL,
			Recipient:         args.Address,
			Data:              &wizard.WizardTransactionData{[]byte("Testnet Faucet Tx"), true},
			Fee:               &wizard.WizardZetherTransactionFee{&wizard.WizardTransactionFee{0, 0, 0, true}, false, 0, 0},
			Amount:            config.FAUCET_TESTNET_COINS_UNITS,
			RingConfiguration: &txs_builder.ZetherRingConfiguration{128, -1},
		}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tx, err := api.txsBuilder.CreateZetherTx(txData, true, true, true, false, ctx, func(status string) {})
	if err != nil {
		return err
	}

	reply.Hash = tx.Bloom.Hash
	return nil

}

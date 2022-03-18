package txs_builder

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"pandora-pay/addresses"
	"pandora-pay/blockchain/blocks/block_complete"
	"pandora-pay/blockchain/data_storage"
	"pandora-pay/blockchain/data_storage/accounts"
	"pandora-pay/blockchain/data_storage/accounts/account"
	"pandora-pay/blockchain/data_storage/plain_accounts/plain_account/asset_fee_liquidity"
	"pandora-pay/blockchain/data_storage/registrations"
	"pandora-pay/blockchain/data_storage/registrations/registration"
	"pandora-pay/blockchain/transactions/transaction"
	"pandora-pay/config/config_coins"
	"pandora-pay/config/config_reward"
	"pandora-pay/cryptography/bn256"
	"pandora-pay/cryptography/crypto"
	"pandora-pay/gui"
	"pandora-pay/helpers"
	"pandora-pay/network/websocks/connection/advanced_connection_types"
	"pandora-pay/store"
	"pandora-pay/store/store_db/store_db_interface"
	"pandora-pay/txs_builder/wizard"
	"pandora-pay/wallet/wallet_address"
)

func (builder *TxsBuilder) getRandomAccount(accs *accounts.Accounts, regs *registrations.Registrations) (addr *addresses.Address, acc *account.Account, reg *registration.Registration, err error) {

	if acc, err = accs.GetRandomAccount(); err != nil {
		return nil, nil, nil, err
	}
	if acc == nil {
		return nil, nil, nil, errors.New("Error getting any random account")
	}

	if reg, err = regs.GetRegistration(acc.PublicKey); err != nil {
		return nil, nil, nil, err
	}

	if addr, err = addresses.CreateAddr(acc.PublicKey, false, nil, nil, nil, 0, nil); err != nil {
		return nil, nil, nil, err
	}

	return
}

func (builder *TxsBuilder) presetZetherRing(ringConfiguration *ZetherRingConfiguration) error {

	if ringConfiguration.RingSize == -1 {
		probability := rand.Intn(1000)
		if probability < 400 {
			ringConfiguration.RingSize = 32
		} else if probability < 600 {
			ringConfiguration.RingSize = 64
		} else if probability < 800 {
			ringConfiguration.RingSize = 128
		} else {
			ringConfiguration.RingSize = 256
		}
	}
	if ringConfiguration.RecipientRingType.NewAccounts == -1 {
		probability := rand.Intn(1000)
		if probability < 800 {
			ringConfiguration.RecipientRingType.NewAccounts = 0
		} else if probability < 900 {
			ringConfiguration.RecipientRingType.NewAccounts = 1
		} else {
			ringConfiguration.RecipientRingType.NewAccounts = 2
		}
	}

	if ringConfiguration.RingSize < 0 {
		return errors.New("number is negative")
	}
	if !crypto.IsPowerOf2(ringConfiguration.RingSize) {
		return errors.New("ring size is not a power of 2")
	}
	if ringConfiguration.RecipientRingType.NewAccounts < 0 || ringConfiguration.RecipientRingType.NewAccounts > ringConfiguration.RingSize/2-1 {
		return errors.New("New accounts needs to be in the interval [0, ringSize-2] ")
	}

	return nil
}

func (builder *TxsBuilder) createZetherRing(sender, receiver *string, assetId []byte, ringConfiguration *ZetherRingConfiguration, dataStorage *data_storage.DataStorage) ([]string, []string, error) {

	var addr *addresses.Address
	var reg *registration.Registration

	var err error

	var accs *accounts.Accounts
	if accs, err = dataStorage.AccsCollection.GetMap(assetId); err != nil {
		return nil, nil, err
	}

	alreadyUsed := make(map[string]bool)

	setAddress := func(address *string, requireDelegatedAccounts bool) (err error) {
		if *address == "" {
			if accs.Count == uint64(len(alreadyUsed)) {
				return errors.New("Accounts have only member. Impossible to get random recipient")
			}
			for {
				if addr, _, reg, err = builder.getRandomAccount(accs, dataStorage.Regs); err != nil {
					return err
				}
				if (requireDelegatedAccounts && !reg.Stakable) || (!requireDelegatedAccounts && len(reg.SpendPublicKey) > 0) {
					continue
				}
				if alreadyUsed[string(addr.PublicKey)] {
					continue
				}
				*address = addr.EncodeAddr()
				break
			}
		} else {
			if addr, err = addresses.DecodeAddr(*address); err != nil {
				return err
			}
		}
		if alreadyUsed[string(addr.PublicKey)] {
			return errors.New("Address was used before")
		}
		alreadyUsed[string(addr.PublicKey)] = true
		return nil
	}

	includeMembers := func(ring *[]string, includeMembers []string) (err error) {
		if includeMembers != nil {
			for _, member := range includeMembers {
				if addr, err = addresses.DecodeAddr(member); err != nil {
					return err
				}
				if alreadyUsed[string(addr.PublicKey)] {
					continue
				}
				alreadyUsed[string(addr.PublicKey)] = true
				*ring = append(*ring, addr.EncodeAddr())
			}
		}
		return nil
	}

	newAccounts := func(ring *[]string, newAccounts int) (err error) {
		for i := 0; i < newAccounts && len(*ring) < ringConfiguration.RingSize/2-1; i++ {
			priv := addresses.GenerateNewPrivateKey()
			if addr, err = priv.GenerateAddress(false, nil, true, nil, 0, nil); err != nil {
				return
			}
			if alreadyUsed[string(addr.PublicKey)] {
				i--
				continue
			}
			alreadyUsed[string(addr.PublicKey)] = true
			*ring = append(*ring, addr.EncodeAddr())
		}
		return
	}

	newRandomAccounts := func(ring *[]string, requireDelegatedAccounts bool) (err error) {

		for len(*ring) < ringConfiguration.RingSize/2-1 {

			if accs.Count <= uint64(len(alreadyUsed)) {
				priv := addresses.GenerateNewPrivateKey()
				if addr, err = priv.GenerateAddress(false, nil, true, nil, 0, nil); err != nil {
					return
				}
			} else {
				if addr, _, reg, err = builder.getRandomAccount(accs, dataStorage.Regs); err != nil {
					return
				}
				if (requireDelegatedAccounts && !reg.Stakable) || (!requireDelegatedAccounts && len(reg.SpendPublicKey) > 0) {
					continue
				}
			}

			if alreadyUsed[string(addr.PublicKey)] {
				continue
			}
			alreadyUsed[string(addr.PublicKey)] = true
			*ring = append(*ring, addr.EncodeAddr())
		}

		return
	}

	if err = setAddress(sender, ringConfiguration.SenderRingType.RequireStakedAccounts); err != nil {
		return nil, nil, err
	}
	if err = setAddress(receiver, ringConfiguration.SenderRingType.RequireStakedAccounts); err != nil {
		return nil, nil, err
	}

	senderRing := make([]string, 0)
	recipientRing := make([]string, 0)

	if err = includeMembers(&senderRing, ringConfiguration.SenderRingType.IncludeMembers); err != nil {
		return nil, nil, err
	}
	if err = includeMembers(&recipientRing, ringConfiguration.RecipientRingType.IncludeMembers); err != nil {
		return nil, nil, err
	}

	if err = newAccounts(&senderRing, ringConfiguration.SenderRingType.NewAccounts); err != nil {
		return nil, nil, err
	}
	if err = newAccounts(&recipientRing, ringConfiguration.RecipientRingType.NewAccounts); err != nil {
		return nil, nil, err
	}

	if err = newRandomAccounts(&senderRing, ringConfiguration.SenderRingType.RequireStakedAccounts); err != nil {
		return nil, nil, err
	}
	if err = newRandomAccounts(&recipientRing, ringConfiguration.SenderRingType.RequireStakedAccounts); err != nil {
		return nil, nil, err
	}

	return senderRing, recipientRing, err
}

func (builder *TxsBuilder) prebuild(txData *TxBuilderCreateZetherTxData, pendingTxs []*transaction.Transaction, ctx context.Context, statusCallback func(string)) ([]*wizard.WizardZetherTransfer, map[string]map[string][]byte, map[string]bool, [][]*bn256.G1, map[string]*wizard.WizardZetherPublicKeyIndex, uint64, []byte, error) {

	sendersPrivateKeys := make([]*addresses.PrivateKey, len(txData.Payloads))
	sendersWalletAddresses := make([]*wallet_address.WalletAddress, len(txData.Payloads))
	sendAssets := make([][]byte, len(txData.Payloads))

	for t, payload := range txData.Payloads {

		if payload.Asset == nil {
			payload.Asset = config_coins.NATIVE_ASSET_FULL
		}
		if payload.Data == nil {
			payload.Data = &wizard.WizardTransactionData{[]byte{}, false}
		}
		if payload.RingConfiguration == nil {
			payload.RingConfiguration = &ZetherRingConfiguration{-1, &ZetherSenderRingType{false, nil, 0}, &ZetherRecipientRingType{false, nil, 0}}
		}
		if payload.Fee == nil {
			payload.Fee = &wizard.WizardZetherTransactionFee{&wizard.WizardTransactionFee{0, 0, 0, true}, false, 0, 0}
		}

		sendAssets[t] = payload.Asset
		if payload.Sender == "" {

			sendersPrivateKeys[t] = addresses.GenerateNewPrivateKey()
			addr, err := sendersPrivateKeys[t].GenerateAddress(false, nil, true, nil, 0, nil)
			if err != nil {
				return nil, nil, nil, nil, nil, 0, nil, err
			}
			payload.Sender = addr.EncodeAddr()

		} else {

			addr, err := builder.wallet.GetWalletAddressByEncodedAddress(payload.Sender, true)
			if err != nil {
				return nil, nil, nil, nil, nil, 0, nil, err
			}

			if addr.PrivateKey == nil {
				return nil, nil, nil, nil, nil, 0, nil, errors.New("Can't be used for transactions as the private key is missing")
			}

			sendersPrivateKeys[t] = &addresses.PrivateKey{Key: addr.PrivateKey.Key[:]}
			payload.Sender = addr.AddressRegistrationEncoded
			sendersWalletAddresses[t] = addr
		}

	}

	senderRingMembers := make([][]string, len(txData.Payloads))
	recipientRingMembers := make([][]string, len(txData.Payloads))

	transfers := make([]*wizard.WizardZetherTransfer, len(txData.Payloads))
	emap := wizard.InitializeEmap(sendAssets)
	hasRollovers := make(map[string]bool)

	rings := make([][]*bn256.G1, len(txData.Payloads))
	publicKeyIndexes := make(map[string]*wizard.WizardZetherPublicKeyIndex)

	sendersEncryptedBalances := make([][]byte, len(txData.Payloads))

	for _, payload := range txData.Payloads {
		if err := builder.presetZetherRing(payload.RingConfiguration); err != nil {
			return nil, nil, nil, nil, nil, 0, nil, err
		}
	}

	if err := store.StoreBlockchain.DB.View(func(reader store_db_interface.StoreDBTransactionInterface) (err error) {

		dataStorage := data_storage.NewDataStorage(reader)

		for t, payload := range txData.Payloads {

			if payload.Extra != nil {
				switch payload.Extra.(type) {
				case *wizard.WizardZetherPayloadExtraStakingReward:

					recipientRingMembers[t] = make([]string, len(senderRingMembers[t-1]))
					senderRingMembers[t] = make([]string, len(recipientRingMembers[t-1]))
					copy(recipientRingMembers[t], senderRingMembers[t-1])
					copy(senderRingMembers[t], recipientRingMembers[t-1])
					payload.Recipient = txData.Payloads[t-1].Sender

					sendersPrivateKeys[t] = addresses.GenerateNewPrivateKey()
					var addr *addresses.Address
					if addr, err = sendersPrivateKeys[t].GenerateAddress(false, nil, true, nil, 0, nil); err != nil {
						return
					}
					payload.Sender = addr.EncodeAddr()
					continue
				}
			}

			if senderRingMembers[t], recipientRingMembers[t], err = builder.createZetherRing(&payload.Sender, &payload.Recipient, payload.Asset, payload.RingConfiguration, dataStorage); err != nil {
				return
			}
		}

		return
	}); err != nil {
		return nil, nil, nil, nil, nil, 0, nil, err
	}

	var chainHeight uint64
	var chainKernelHash []byte

	if err := store.StoreBlockchain.DB.View(func(reader store_db_interface.StoreDBTransactionInterface) (err error) {

		dataStorage := data_storage.NewDataStorage(reader)

		chainHeight, _ = binary.Uvarint(reader.Get("chainHeight"))
		chainKernelHash = reader.Get("chainKernelHash")

		for t, payload := range txData.Payloads {

			var accs *accounts.Accounts
			if accs, err = dataStorage.AccsCollection.GetMap(payload.Asset); err != nil {
				return
			}

			if !bytes.Equal(payload.Asset, config_coins.NATIVE_ASSET_FULL) && payload.Fee.Auto {
				var assetFeeLiquidity *asset_fee_liquidity.AssetFeeLiquidity
				if assetFeeLiquidity, err = dataStorage.GetAssetFeeLiquidityTop(payload.Asset); err != nil {
					return
				}
				if assetFeeLiquidity == nil {
					return errors.New("There is no Asset Fee Liquidity for this asset")
				}
				payload.Fee.Rate = assetFeeLiquidity.Rate
				payload.Fee.LeadingZeros = assetFeeLiquidity.LeadingZeros
			}

			transfers[t] = &wizard.WizardZetherTransfer{
				Asset:            payload.Asset,
				SenderPrivateKey: sendersPrivateKeys[t].Key[:],
				Recipient:        payload.Recipient,
				Amount:           payload.Amount,
				Burn:             payload.Burn,
				Data:             payload.Data,
				FeeRate:          payload.Fee.Rate,
				FeeLeadingZeros:  payload.Fee.LeadingZeros,
				PayloadExtra:     payload.Extra,
			}

			var ring []*bn256.G1
			uniqueMap := make(map[string]bool)

			addPoint := func(address string, isSender bool) (err error) {
				var addr *addresses.Address
				var p *crypto.Point

				if addr, err = addresses.DecodeAddr(address); err != nil {
					return
				}
				if uniqueMap[string(addr.PublicKey)] {
					return
				}
				uniqueMap[string(addr.PublicKey)] = true

				if p, err = addr.GetPoint(); err != nil {
					return
				}

				if emap[string(payload.Asset)][p.G1().String()] == nil {

					var acc *account.Account
					var reg *registration.Registration

					if acc, err = accs.GetAccount(addr.PublicKey); err != nil {
						return
					}
					if reg, err = dataStorage.Regs.GetRegistration(addr.PublicKey); err != nil {
						return
					}

					hasRollover := acc != nil && reg.Stakable

					var newBalance *crypto.ElGamal
					if acc != nil {
						newBalance = acc.Balance.Amount
					}

					if newBalance, err = wizard.GetZetherBalance(addr.PublicKey, newBalance, payload.Asset, hasRollover, true, pendingTxs); err != nil {
						return
					}

					if isSender { //sender
						sendersEncryptedBalances[t] = newBalance.Serialize()

						if reg != nil && len(reg.SpendPublicKey) > 0 && payload.Extra == nil {
							transfers[t].SenderUnstakeRequired = true
							if sendersWalletAddresses[t].SpendPrivateKey == nil {
								return errors.New("Spend Private Key is missing")
							}
							if !bytes.Equal(sendersWalletAddresses[t].SpendPublicKey, reg.SpendPublicKey) {
								return errors.New("Wallet Spend Public Key is not matching")
							}
							transfers[t].SenderSpendPrivateKey = sendersWalletAddresses[t].SpendPrivateKey.Key
						}
					}

					emap[string(payload.Asset)][p.G1().String()] = newBalance.Serialize()
					hasRollovers[p.G1().String()] = hasRollover

					if publicKeyIndexes[string(addr.PublicKey)] == nil {

						publicKeyIndex := &wizard.WizardZetherPublicKeyIndex{}
						publicKeyIndexes[string(addr.PublicKey)] = publicKeyIndex

						if reg != nil {
							publicKeyIndex.Registered = true
							publicKeyIndex.RegisteredIndex = reg.Index
						} else {
							if len(addr.Registration) == 0 {
								return fmt.Errorf("Signature is missing for %s", addr.EncodeAddr())
							}
							publicKeyIndex.RegistrationStakable = addr.Stakable
							publicKeyIndex.RegistrationSpendPublicKey = addr.SpendPublicKey
							publicKeyIndex.RegistrationSignature = addr.Registration
						}
					}
				}
				ring = append(ring, p.G1())

				return
			}

			if err = addPoint(payload.Sender, true); err != nil {
				return
			}
			if err = addPoint(payload.Recipient, false); err != nil {
				return
			}
			for _, ringMember := range senderRingMembers[t] {
				if err = addPoint(ringMember, false); err != nil {
					return
				}
			}
			for _, ringMember := range recipientRingMembers[t] {
				if err = addPoint(ringMember, false); err != nil {
					return
				}
			}

			transfers[t].WitnessIndexes = helpers.ShuffleArray_for_Zether(payload.RingConfiguration.RingSize)

			rings[t] = ring
		}
		statusCallback("Wallet Addresses Found")

		return
	}); err != nil {
		return nil, nil, nil, nil, nil, 0, nil, err
	}
	statusCallback("Balances checked")

	for t := range transfers {
		if sendersWalletAddresses[t] == nil {
			transfers[t].SenderDecryptedBalance = transfers[t].Amount
		} else {
			if txData.Payloads[t].DecryptedBalance > 0 {
				decrypted, err := builder.wallet.DecryptBalanceByPublicKey(sendersWalletAddresses[t].PublicKey, sendersEncryptedBalances[t], transfers[t].Asset, true, txData.Payloads[t].DecryptedBalance, true, true, ctx, statusCallback)
				if err != nil {
					return nil, nil, nil, nil, nil, 0, nil, err
				}
				transfers[t].SenderDecryptedBalance = decrypted
			} else {
				decrypted, err := builder.wallet.DecryptBalanceByPublicKey(sendersWalletAddresses[t].PublicKey, sendersEncryptedBalances[t], transfers[t].Asset, false, 0, true, true, ctx, statusCallback)
				if err != nil {
					return nil, nil, nil, nil, nil, 0, nil, err
				}
				transfers[t].SenderDecryptedBalance = decrypted
			}

		}
		if transfers[t].SenderDecryptedBalance == 0 {
			return nil, nil, nil, nil, nil, 0, nil, errors.New("You have no funds")
		}
		if transfers[t].SenderDecryptedBalance < txData.Payloads[t].Amount {
			return nil, nil, nil, nil, nil, 0, nil, errors.New("Not enough funds")
		}
	}

	statusCallback("Balances decoded")

	return transfers, emap, hasRollovers, rings, publicKeyIndexes, chainHeight, chainKernelHash, nil
}

func (builder *TxsBuilder) CreateZetherTx(txData *TxBuilderCreateZetherTxData, pendingTxs []*transaction.Transaction, propagateTx, awaitAnswer, awaitBroadcast bool, validateTx bool, ctx context.Context, statusCallback func(string)) (*transaction.Transaction, error) {

	if pendingTxs == nil {
		pendingTxs = builder.mempool.Txs.GetTxsOnlyList()
	}

	builder.lock.Lock()
	defer builder.lock.Unlock()

	transfers, emap, hasRollovers, ringMembers, publicKeyIndexes, chainHeight, chainKernelHash, err := builder.prebuild(txData, pendingTxs, ctx, statusCallback)
	if err != nil {
		return nil, err
	}

	feesFinal := make([]*wizard.WizardTransactionFee, len(txData.Payloads))
	for t, payload := range txData.Payloads {
		feesFinal[t] = payload.Fee.WizardTransactionFee
	}

	var tx *transaction.Transaction
	if tx, err = wizard.CreateZetherTx(transfers, emap, hasRollovers, ringMembers, chainHeight-1, chainKernelHash, publicKeyIndexes, feesFinal, ctx, statusCallback); err != nil {
		return nil, err
	}

	if err = builder.txsValidator.MarkAsValidatedTx(tx); err != nil {
		return nil, err
	}

	if propagateTx {
		if err = builder.mempool.AddTxToMempool(tx, chainHeight, true, awaitAnswer, awaitBroadcast, advanced_connection_types.UUID_ALL, ctx); err != nil {
			return nil, err
		}
	}

	return tx, nil
}

func (builder *TxsBuilder) CreateForgingTransactions(blkComplete *block_complete.BlockComplete, forgerPublicKey []byte, decryptedBalance uint64, pendingTxs []*transaction.Transaction) (*transaction.Transaction, error) {

	if pendingTxs == nil {
		pendingTxs = builder.mempool.Txs.GetTxsOnlyList()
	}

	gui.GUI.Info("CreateForgingTransactions 1")
	forger, err := addresses.CreateAddr(forgerPublicKey, false, nil, nil, nil, 0, nil)
	if err != nil {
		return nil, err
	}

	reward := config_reward.GetRewardAt(blkComplete.Height)

	chainHeight := blkComplete.Height
	if chainHeight > 0 {
		chainHeight--
	}

	builder.lock.Lock()
	defer builder.lock.Unlock()

	//reward
	txData := &TxBuilderCreateZetherTxData{
		Payloads: []*TxBuilderCreateZetherTxPayload{
			{
				forger.EncodeAddr(),
				config_coins.NATIVE_ASSET_FULL,
				0,
				decryptedBalance,
				"",
				blkComplete.StakingAmount,
				&ZetherRingConfiguration{32, &ZetherSenderRingType{true, nil, 0}, &ZetherRecipientRingType{true, nil, 0}},
				nil,
				&wizard.WizardZetherTransactionFee{&wizard.WizardTransactionFee{0, 0, 0, false}, false, 0, 0},
				&wizard.WizardZetherPayloadExtraStaking{},
			},
			{
				"",
				config_coins.NATIVE_ASSET_FULL,
				reward,
				reward, //reward will be the encrypted Balance
				forger.EncodeAddr(),
				0,
				&ZetherRingConfiguration{32, &ZetherSenderRingType{true, nil, 0}, &ZetherRecipientRingType{true, nil, 0}},
				nil,
				&wizard.WizardZetherTransactionFee{&wizard.WizardTransactionFee{0, 0, 0, false}, false, 0, 0},
				&wizard.WizardZetherPayloadExtraStakingReward{nil, reward},
			},
		},
	}

	transfers, emap, hasRollovers, ringMembers, publicKeyIndexes, _, chainKernelHash2, err := builder.prebuild(txData, pendingTxs, context.Background(), func(string) {})
	if err != nil {
		return nil, err
	}

	if chainHeight == 0 {
		chainKernelHash2 = blkComplete.PrevKernelHash
	}

	if !bytes.Equal(chainKernelHash2, blkComplete.PrevKernelHash) {
		return nil, errors.New("Block already changed")
	}

	gui.GUI.Info("CreateForgingTransactions 2")

	feesFinal := make([]*wizard.WizardTransactionFee, len(txData.Payloads))
	for t, payload := range txData.Payloads {
		feesFinal[t] = payload.Fee.WizardTransactionFee
	}

	var tx *transaction.Transaction
	if tx, err = wizard.CreateZetherTx(transfers, emap, hasRollovers, ringMembers, chainHeight, chainKernelHash2, publicKeyIndexes, feesFinal, context.Background(), func(string) {}); err != nil {
		return nil, err
	}

	gui.GUI.Info("CreateForgingTransactions 3")

	if err = builder.txsValidator.MarkAsValidatedTx(tx); err != nil {
		return nil, err
	}

	gui.GUI.Info("CreateForgingTransactions 4")

	return tx, nil
}

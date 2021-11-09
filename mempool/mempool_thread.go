package mempool

import (
	"errors"
	"pandora-pay/blockchain/data_storage"
	"pandora-pay/blockchain/transactions/transaction/transaction_type"
	"pandora-pay/blockchain/transactions/transaction/transaction_zether"
	"pandora-pay/config"
	"pandora-pay/store"
	"pandora-pay/store/store_db/store_db_interface"
	"sync/atomic"
)

type mempoolWork struct {
	chainHash   []byte         `json:"-"` //32 byte
	chainHeight uint64         `json:"-"`
	result      *MempoolResult `json:"-"`
}

type mempoolWorker struct {
	dbTx store_db_interface.StoreDBTransactionInterface `json:"-"`
}

type MempoolWorkerAddTx struct {
	Tx     *mempoolTx
	Result chan<- error
}

type MempoolWorkerRemoveTxs struct {
	Txs    []string
	Result chan<- bool
}

type MempoolWorkerInsertTxs struct {
	Txs    []*mempoolTx
	Result chan<- bool
}

//process the worker for transactions to prepare the transactions to the forger
func (worker *mempoolWorker) processing(
	newWorkCn <-chan *mempoolWork,
	suspendProcessingCn <-chan struct{},
	continueProcessingCn <-chan ContinueProcessingType,
	addTransactionCn <-chan *MempoolWorkerAddTx,
	insertTransactionsCn <-chan *MempoolWorkerInsertTxs,
	removeTransactionsCn <-chan *MempoolWorkerRemoveTxs,
	txs *MempoolTxs,
) {

	var work *mempoolWork

	txsList := []*mempoolTx{}
	txsMap := make(map[string]*mempoolTx)
	txsMapVerified := make(map[string]bool)
	listIndex := 0

	var dataStorage *data_storage.DataStorage

	includedTotalSize := uint64(0)
	includedTxs := []*mempoolTx{}
	includedZetherNonceMap := make(map[string]bool)

	resetNow := func(newWork *mempoolWork) {

		if newWork.chainHash != nil {
			txsMapVerified = make(map[string]bool)
			dataStorage = nil
			work = newWork
			includedTotalSize = uint64(0)
			includedTxs = []*mempoolTx{}
			includedZetherNonceMap = make(map[string]bool)
			listIndex = 0
			if len(txsList) > 1 {
				sortTxs(txsList)
			}
		}
	}

	removeTxs := func(data *MempoolWorkerRemoveTxs) {
		result := false

		removedTxsMap := make(map[string]bool)
		for _, hash := range data.Txs {
			if hash != "" && txsMap[hash] != nil {
				removedTxsMap[hash] = true
				txs.deleted(txsMap[hash], true)
				delete(txsMap, hash)
				txs.deleteTx(hash)
				result = true
			}
		}
		if len(removedTxsMap) > 0 {

			newLength := 0
			for _, tx := range txsList {
				if !removedTxsMap[tx.Tx.Bloom.HashStr] {
					newLength += 1
				}
			}

			newList := make([]*mempoolTx, newLength)
			c := 0
			for _, tx := range txsList {
				if !removedTxsMap[tx.Tx.Bloom.HashStr] {
					newList[c] = tx
					c += 1
				}
			}
			txsList = newList
		}

		data.Result <- result
	}

	insertTxs := func(data *MempoolWorkerInsertTxs) {
		result := false
		for _, tx := range data.Txs {
			if tx != nil && txsMap[tx.Tx.Bloom.HashStr] == nil {
				txsMap[tx.Tx.Bloom.HashStr] = tx
				txs.insertTx(tx)
				txs.inserted(tx)
				txsList = append(txsList, tx)
				result = true
			}
		}
		data.Result <- result
	}

	suspended := false
	for {

		select {
		case <-suspendProcessingCn:
			suspended = true
			continue
		case newWork := <-newWorkCn:
			resetNow(newWork)
		case data := <-removeTransactionsCn:
			removeTxs(data)
		case data := <-insertTransactionsCn:
			insertTxs(data)
		case continueProcessingType := <-continueProcessingCn:

			suspended = false

			switch continueProcessingType {
			case CONTINUE_PROCESSING_ERROR:
			case CONTINUE_PROCESSING_NO_ERROR:
				work = nil //it needs a new work
			case CONTINUE_PROCESSING_NO_ERROR_RESET:
				dataStorage = nil
				listIndex = 0
			}

		}

		if work == nil || suspended { //if no work was sent, just loop again
			continue
		}

		//let's check hf the work has been changed
		store.StoreBlockchain.DB.View(func(dbTx store_db_interface.StoreDBTransactionInterface) (err error) {

			if dataStorage != nil {
				dataStorage.SetTx(dbTx)
			}

			for {

				if dataStorage == nil {
					dataStorage = data_storage.NewDataStorage(dbTx)
				}

				select {
				case <-suspendProcessingCn:
					suspended = true
					return
				case newWork := <-newWorkCn:
					resetNow(newWork)
				default:

					var tx *mempoolTx
					var newAddTx *MempoolWorkerAddTx

					if listIndex == len(txsList) {

						select {
						case newWork := <-newWorkCn:
							resetNow(newWork)
							continue
						case <-suspendProcessingCn:
							suspended = true
							return
						case newAddTx = <-addTransactionCn:
							tx = newAddTx.Tx
							if txsMap[tx.Tx.Bloom.HashStr] != nil {
								if newAddTx.Result != nil {
									newAddTx.Result <- errors.New("Already found")
								}
								continue
							}
						}

					} else {
						tx = txsList[listIndex]
						listIndex += 1
					}

					if tx == nil {
						continue
					}

					var finalErr error

					if txsMapVerified[tx.Tx.Bloom.HashStr] {
						finalErr = errors.New("Already processed")
					} else {

						txsMapVerified[tx.Tx.Bloom.HashStr] = true

						if exists := dbTx.Exists("txHash:" + string(tx.Tx.Bloom.HashStr)); exists {
							finalErr = errors.New("Tx is already included in blockchain")
						}

						if finalErr == nil && tx.Tx.Version == transaction_type.TX_ZETHER {
							base := tx.Tx.TransactionBaseInterface.(*transaction_zether.TransactionZether)

							for t := range base.Payloads {
								if includedZetherNonceMap[string(base.Bloom.Nonces[t])] {
									finalErr = errors.New("Zether Nonce exists")
								}
							}
						}

						//was rejected by mempool nonce map
						if finalErr != nil {

						} else if finalErr = tx.Tx.IncludeTransaction(work.chainHeight, dataStorage); finalErr != nil {
							dataStorage.Rollback()
						} else {

							if includedTotalSize+tx.Tx.Bloom.Size < config.BLOCK_MAX_SIZE {

								includedTotalSize += tx.Tx.Bloom.Size
								includedTxs = append(includedTxs, tx)

								atomic.StoreUint64(&work.result.totalSize, includedTotalSize)
								work.result.txs.Store(includedTxs)

								if tx.Tx.Version == transaction_type.TX_ZETHER {
									base := tx.Tx.TransactionBaseInterface.(*transaction_zether.TransactionZether)

									for t := range base.Payloads {
										includedZetherNonceMap[string(base.Bloom.Nonces[t])] = true
									}
								}

								if err = dataStorage.CommitChanges(); err != nil {
									return
								}

							} else {
								dataStorage.Rollback()
							}

							if newAddTx != nil {
								txsList = append(txsList, newAddTx.Tx)
								listIndex += 1
								txsMap[tx.Tx.Bloom.HashStr] = newAddTx.Tx
								txs.inserted(tx)
							}

						}

					}

					if finalErr != nil {
						if newAddTx == nil {
							//removing
							//this is done because listIndex was incremented already before
							txsList = append(txsList[:listIndex-1], txsList[listIndex:]...)
							listIndex--
							txs.deleted(tx, false)
						}
						delete(txsMap, tx.Tx.Bloom.HashStr)
						txs.deleteTx(tx.Tx.Bloom.HashStr)
					}

					if newAddTx != nil && newAddTx.Result != nil {
						newAddTx.Result <- finalErr
					}

				}
			}

		})

	}
}

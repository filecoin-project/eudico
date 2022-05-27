# Groups
* [](#)
  * [Closing](#Closing)
  * [Discover](#Discover)
  * [Session](#Session)
  * [Shutdown](#Shutdown)
  * [Version](#Version)
* [Abort](#Abort)
  * [AbortAtomicExec](#AbortAtomicExec)
* [Add](#Add)
  * [AddSubnet](#AddSubnet)
* [Auth](#Auth)
  * [AuthNew](#AuthNew)
  * [AuthVerify](#AuthVerify)
* [Beacon](#Beacon)
  * [BeaconGetEntry](#BeaconGetEntry)
* [Chain](#Chain)
  * [ChainBlockstoreInfo](#ChainBlockstoreInfo)
  * [ChainCheckBlockstore](#ChainCheckBlockstore)
  * [ChainDeleteObj](#ChainDeleteObj)
  * [ChainExport](#ChainExport)
  * [ChainGetBlock](#ChainGetBlock)
  * [ChainGetBlockMessages](#ChainGetBlockMessages)
  * [ChainGetGenesis](#ChainGetGenesis)
  * [ChainGetMessage](#ChainGetMessage)
  * [ChainGetMessagesInTipset](#ChainGetMessagesInTipset)
  * [ChainGetNode](#ChainGetNode)
  * [ChainGetParentMessages](#ChainGetParentMessages)
  * [ChainGetParentReceipts](#ChainGetParentReceipts)
  * [ChainGetPath](#ChainGetPath)
  * [ChainGetTipSet](#ChainGetTipSet)
  * [ChainGetTipSetAfterHeight](#ChainGetTipSetAfterHeight)
  * [ChainGetTipSetByHeight](#ChainGetTipSetByHeight)
  * [ChainHasObj](#ChainHasObj)
  * [ChainHead](#ChainHead)
  * [ChainNotify](#ChainNotify)
  * [ChainReadObj](#ChainReadObj)
  * [ChainSetHead](#ChainSetHead)
  * [ChainStatObj](#ChainStatObj)
  * [ChainTipSetWeight](#ChainTipSetWeight)
* [Client](#Client)
  * [ClientCalcCommP](#ClientCalcCommP)
  * [ClientCancelDataTransfer](#ClientCancelDataTransfer)
  * [ClientCancelRetrievalDeal](#ClientCancelRetrievalDeal)
  * [ClientDataTransferUpdates](#ClientDataTransferUpdates)
  * [ClientDealPieceCID](#ClientDealPieceCID)
  * [ClientDealSize](#ClientDealSize)
  * [ClientExport](#ClientExport)
  * [ClientFindData](#ClientFindData)
  * [ClientGenCar](#ClientGenCar)
  * [ClientGetDealInfo](#ClientGetDealInfo)
  * [ClientGetDealStatus](#ClientGetDealStatus)
  * [ClientGetDealUpdates](#ClientGetDealUpdates)
  * [ClientGetRetrievalUpdates](#ClientGetRetrievalUpdates)
  * [ClientHasLocal](#ClientHasLocal)
  * [ClientImport](#ClientImport)
  * [ClientListDataTransfers](#ClientListDataTransfers)
  * [ClientListDeals](#ClientListDeals)
  * [ClientListImports](#ClientListImports)
  * [ClientListRetrievals](#ClientListRetrievals)
  * [ClientMinerQueryOffer](#ClientMinerQueryOffer)
  * [ClientQueryAsk](#ClientQueryAsk)
  * [ClientRemoveImport](#ClientRemoveImport)
  * [ClientRestartDataTransfer](#ClientRestartDataTransfer)
  * [ClientRetrieve](#ClientRetrieve)
  * [ClientRetrieveTryRestartInsufficientFunds](#ClientRetrieveTryRestartInsufficientFunds)
  * [ClientRetrieveWait](#ClientRetrieveWait)
  * [ClientStartDeal](#ClientStartDeal)
  * [ClientStatelessDeal](#ClientStatelessDeal)
* [Compute](#Compute)
  * [ComputeAndSubmitExec](#ComputeAndSubmitExec)
* [Create](#Create)
  * [CreateBackup](#CreateBackup)
* [Cross](#Cross)
  * [CrossMsgResolve](#CrossMsgResolve)
* [Fund](#Fund)
  * [FundSubnet](#FundSubnet)
* [Gas](#Gas)
  * [GasEstimateFeeCap](#GasEstimateFeeCap)
  * [GasEstimateGasLimit](#GasEstimateGasLimit)
  * [GasEstimateGasPremium](#GasEstimateGasPremium)
  * [GasEstimateMessageGas](#GasEstimateMessageGas)
* [Get](#Get)
  * [GetCrossMsgsPool](#GetCrossMsgsPool)
  * [GetUnverifiedCrossMsgsPool](#GetUnverifiedCrossMsgsPool)
* [I](#I)
  * [ID](#ID)
* [Init](#Init)
  * [InitAtomicExec](#InitAtomicExec)
* [Join](#Join)
  * [JoinSubnet](#JoinSubnet)
* [Kill](#Kill)
  * [KillSubnet](#KillSubnet)
* [Leave](#Leave)
  * [LeaveSubnet](#LeaveSubnet)
* [List](#List)
  * [ListAtomicExecs](#ListAtomicExecs)
  * [ListCheckpoints](#ListCheckpoints)
  * [ListSubnets](#ListSubnets)
* [Lock](#Lock)
  * [LockState](#LockState)
* [Log](#Log)
  * [LogAlerts](#LogAlerts)
  * [LogList](#LogList)
  * [LogSetLevel](#LogSetLevel)
* [Market](#Market)
  * [MarketAddBalance](#MarketAddBalance)
  * [MarketGetReserved](#MarketGetReserved)
  * [MarketReleaseFunds](#MarketReleaseFunds)
  * [MarketReserveFunds](#MarketReserveFunds)
  * [MarketWithdraw](#MarketWithdraw)
* [Mine](#Mine)
  * [MineSubnet](#MineSubnet)
* [Miner](#Miner)
  * [MinerCreateBlock](#MinerCreateBlock)
  * [MinerGetBaseInfo](#MinerGetBaseInfo)
* [Mpool](#Mpool)
  * [MpoolBatchPush](#MpoolBatchPush)
  * [MpoolBatchPushMessage](#MpoolBatchPushMessage)
  * [MpoolBatchPushUntrusted](#MpoolBatchPushUntrusted)
  * [MpoolCheckMessages](#MpoolCheckMessages)
  * [MpoolCheckPendingMessages](#MpoolCheckPendingMessages)
  * [MpoolCheckReplaceMessages](#MpoolCheckReplaceMessages)
  * [MpoolClear](#MpoolClear)
  * [MpoolGetConfig](#MpoolGetConfig)
  * [MpoolGetNonce](#MpoolGetNonce)
  * [MpoolPending](#MpoolPending)
  * [MpoolPush](#MpoolPush)
  * [MpoolPushMessage](#MpoolPushMessage)
  * [MpoolPushUntrusted](#MpoolPushUntrusted)
  * [MpoolSelect](#MpoolSelect)
  * [MpoolSetConfig](#MpoolSetConfig)
  * [MpoolSub](#MpoolSub)
* [Msig](#Msig)
  * [MsigAddApprove](#MsigAddApprove)
  * [MsigAddCancel](#MsigAddCancel)
  * [MsigAddPropose](#MsigAddPropose)
  * [MsigApprove](#MsigApprove)
  * [MsigApproveTxnHash](#MsigApproveTxnHash)
  * [MsigCancel](#MsigCancel)
  * [MsigCancelTxnHash](#MsigCancelTxnHash)
  * [MsigCreate](#MsigCreate)
  * [MsigGetAvailableBalance](#MsigGetAvailableBalance)
  * [MsigGetPending](#MsigGetPending)
  * [MsigGetVested](#MsigGetVested)
  * [MsigGetVestingSchedule](#MsigGetVestingSchedule)
  * [MsigPropose](#MsigPropose)
  * [MsigRemoveSigner](#MsigRemoveSigner)
  * [MsigSwapApprove](#MsigSwapApprove)
  * [MsigSwapCancel](#MsigSwapCancel)
  * [MsigSwapPropose](#MsigSwapPropose)
* [Net](#Net)
  * [NetAddrsListen](#NetAddrsListen)
  * [NetAgentVersion](#NetAgentVersion)
  * [NetAutoNatStatus](#NetAutoNatStatus)
  * [NetBandwidthStats](#NetBandwidthStats)
  * [NetBandwidthStatsByPeer](#NetBandwidthStatsByPeer)
  * [NetBandwidthStatsByProtocol](#NetBandwidthStatsByProtocol)
  * [NetBlockAdd](#NetBlockAdd)
  * [NetBlockList](#NetBlockList)
  * [NetBlockRemove](#NetBlockRemove)
  * [NetConnect](#NetConnect)
  * [NetConnectedness](#NetConnectedness)
  * [NetDisconnect](#NetDisconnect)
  * [NetFindPeer](#NetFindPeer)
  * [NetLimit](#NetLimit)
  * [NetPeerInfo](#NetPeerInfo)
  * [NetPeers](#NetPeers)
  * [NetPing](#NetPing)
  * [NetProtectAdd](#NetProtectAdd)
  * [NetProtectList](#NetProtectList)
  * [NetProtectRemove](#NetProtectRemove)
  * [NetPubsubScores](#NetPubsubScores)
  * [NetSetLimit](#NetSetLimit)
  * [NetStat](#NetStat)
* [Node](#Node)
  * [NodeStatus](#NodeStatus)
* [Paych](#Paych)
  * [PaychAllocateLane](#PaychAllocateLane)
  * [PaychAvailableFunds](#PaychAvailableFunds)
  * [PaychAvailableFundsByFromTo](#PaychAvailableFundsByFromTo)
  * [PaychCollect](#PaychCollect)
  * [PaychFund](#PaychFund)
  * [PaychGet](#PaychGet)
  * [PaychGetWaitReady](#PaychGetWaitReady)
  * [PaychList](#PaychList)
  * [PaychNewPayment](#PaychNewPayment)
  * [PaychSettle](#PaychSettle)
  * [PaychStatus](#PaychStatus)
  * [PaychVoucherAdd](#PaychVoucherAdd)
  * [PaychVoucherCheckSpendable](#PaychVoucherCheckSpendable)
  * [PaychVoucherCheckValid](#PaychVoucherCheckValid)
  * [PaychVoucherCreate](#PaychVoucherCreate)
  * [PaychVoucherList](#PaychVoucherList)
  * [PaychVoucherSubmit](#PaychVoucherSubmit)
* [Release](#Release)
  * [ReleaseFunds](#ReleaseFunds)
* [State](#State)
  * [StateAccountKey](#StateAccountKey)
  * [StateAllMinerFaults](#StateAllMinerFaults)
  * [StateCall](#StateCall)
  * [StateChangedActors](#StateChangedActors)
  * [StateCirculatingSupply](#StateCirculatingSupply)
  * [StateCompute](#StateCompute)
  * [StateDealProviderCollateralBounds](#StateDealProviderCollateralBounds)
  * [StateDecodeParams](#StateDecodeParams)
  * [StateEncodeParams](#StateEncodeParams)
  * [StateGetActor](#StateGetActor)
  * [StateGetNetworkParams](#StateGetNetworkParams)
  * [StateGetRandomnessFromBeacon](#StateGetRandomnessFromBeacon)
  * [StateGetRandomnessFromTickets](#StateGetRandomnessFromTickets)
  * [StateListActors](#StateListActors)
  * [StateListMessages](#StateListMessages)
  * [StateListMiners](#StateListMiners)
  * [StateLookupID](#StateLookupID)
  * [StateLookupRobustAddress](#StateLookupRobustAddress)
  * [StateMarketBalance](#StateMarketBalance)
  * [StateMarketDeals](#StateMarketDeals)
  * [StateMarketParticipants](#StateMarketParticipants)
  * [StateMarketStorageDeal](#StateMarketStorageDeal)
  * [StateMinerActiveSectors](#StateMinerActiveSectors)
  * [StateMinerAvailableBalance](#StateMinerAvailableBalance)
  * [StateMinerDeadlines](#StateMinerDeadlines)
  * [StateMinerFaults](#StateMinerFaults)
  * [StateMinerInfo](#StateMinerInfo)
  * [StateMinerInitialPledgeCollateral](#StateMinerInitialPledgeCollateral)
  * [StateMinerPartitions](#StateMinerPartitions)
  * [StateMinerPower](#StateMinerPower)
  * [StateMinerPreCommitDepositForPower](#StateMinerPreCommitDepositForPower)
  * [StateMinerProvingDeadline](#StateMinerProvingDeadline)
  * [StateMinerRecoveries](#StateMinerRecoveries)
  * [StateMinerSectorAllocated](#StateMinerSectorAllocated)
  * [StateMinerSectorCount](#StateMinerSectorCount)
  * [StateMinerSectors](#StateMinerSectors)
  * [StateNetworkName](#StateNetworkName)
  * [StateNetworkVersion](#StateNetworkVersion)
  * [StateReadState](#StateReadState)
  * [StateReplay](#StateReplay)
  * [StateSearchMsg](#StateSearchMsg)
  * [StateSectorExpiration](#StateSectorExpiration)
  * [StateSectorGetInfo](#StateSectorGetInfo)
  * [StateSectorPartition](#StateSectorPartition)
  * [StateSectorPreCommitInfo](#StateSectorPreCommitInfo)
  * [StateVMCirculatingSupplyInternal](#StateVMCirculatingSupplyInternal)
  * [StateVerifiedClientStatus](#StateVerifiedClientStatus)
  * [StateVerifiedRegistryRootKey](#StateVerifiedRegistryRootKey)
  * [StateVerifierStatus](#StateVerifierStatus)
  * [StateWaitMsg](#StateWaitMsg)
* [Subnet](#Subnet)
  * [SubnetChainHead](#SubnetChainHead)
  * [SubnetChainNotify](#SubnetChainNotify)
  * [SubnetStateGetActor](#SubnetStateGetActor)
  * [SubnetStateGetValidators](#SubnetStateGetValidators)
  * [SubnetStateWaitMsg](#SubnetStateWaitMsg)
* [Sync](#Sync)
  * [SyncBlock](#SyncBlock)
  * [SyncCheckBad](#SyncCheckBad)
  * [SyncCheckpoint](#SyncCheckpoint)
  * [SyncIncomingBlocks](#SyncIncomingBlocks)
  * [SyncMarkBad](#SyncMarkBad)
  * [SyncState](#SyncState)
  * [SyncSubmitBlock](#SyncSubmitBlock)
  * [SyncSubnet](#SyncSubnet)
  * [SyncUnmarkAllBad](#SyncUnmarkAllBad)
  * [SyncUnmarkBad](#SyncUnmarkBad)
  * [SyncValidateTipset](#SyncValidateTipset)
* [Unlock](#Unlock)
  * [UnlockState](#UnlockState)
* [Validate](#Validate)
  * [ValidateCheckpoint](#ValidateCheckpoint)
* [Wallet](#Wallet)
  * [WalletBalance](#WalletBalance)
  * [WalletDefaultAddress](#WalletDefaultAddress)
  * [WalletDelete](#WalletDelete)
  * [WalletExport](#WalletExport)
  * [WalletHas](#WalletHas)
  * [WalletImport](#WalletImport)
  * [WalletList](#WalletList)
  * [WalletNew](#WalletNew)
  * [WalletSetDefault](#WalletSetDefault)
  * [WalletSign](#WalletSign)
  * [WalletSignMessage](#WalletSignMessage)
  * [WalletValidateAddress](#WalletValidateAddress)
  * [WalletVerify](#WalletVerify)
## 


### Closing


Perms: read

Inputs: `null`

Response: `{}`

### Discover


Perms: read

Inputs: `null`

Response:
```json
{
  "info": {
    "title": "Lotus RPC API",
    "version": "1.2.1/generated=2020-11-22T08:22:42-06:00"
  },
  "methods": [],
  "openrpc": "1.2.6"
}
```

### Session


Perms: read

Inputs: `null`

Response: `"07070707-0707-0707-0707-070707070707"`

### Shutdown


Perms: admin

Inputs: `null`

Response: `{}`

### Version


Perms: read

Inputs: `null`

Response:
```json
{
  "Version": "string value",
  "APIVersion": 131584,
  "BlockDelay": 42
}
```

## Abort


### AbortAtomicExec



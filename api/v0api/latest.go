package v0api

import (
	"github.com/filecoin-project/lotus/api"
)

type Common = api.Common
type HierarchicalCns = api.HierarchicalCns
type EudicoStats = api.EudicoStats

type Net = api.Net
type CommonNet = api.CommonNet

type CommonStruct = api.CommonStruct
type CommonStub = api.CommonStub
type NetStruct = api.NetStruct
type NetStub = api.NetStub

type HierarchicalCnsStruct = api.HierarchicalCnsStruct
type HierarchicalCnsStub = api.HierarchicalCnsStub
type EudicoStatsStruct = api.EudicoStatsStruct
type EudicoStatsStub = api.EudicoStatsStub

type CommonNetStruct = api.CommonNetStruct
type CommonNetStub = api.CommonNetStub

type StorageMiner = api.StorageMiner
type StorageMinerStruct = api.StorageMinerStruct

type Worker = api.Worker
type WorkerStruct = api.WorkerStruct

type Wallet = api.Wallet

func PermissionedStorMinerAPI(a StorageMiner) StorageMiner {
	return api.PermissionedStorMinerAPI(a)
}

func PermissionedWorkerAPI(a Worker) Worker {
	return api.PermissionedWorkerAPI(a)
}

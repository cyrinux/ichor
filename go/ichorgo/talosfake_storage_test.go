package ichorgo

import (
	"context"

	storageapi "github.com/siderolabs/talos/pkg/machinery/api/storage"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeTalosStorage answers BlockDeviceWipe: it records each request; the node's wipeErr
// answers instead when set, and a node whose version is before v1.9 answers Unimplemented.
type fakeTalosStorage struct {
	storageapi.UnimplementedStorageServiceServer

	f *fakeTalos
}

func (st fakeTalosStorage) BlockDeviceWipe(ctx context.Context, req *storageapi.BlockDeviceWipeRequest) (*storageapi.BlockDeviceWipeResponse, error) {
	node, err := st.f.enter(ctx, "BlockDeviceWipe")
	if err != nil {
		return nil, err
	}

	st.f.mu.Lock()
	defer st.f.mu.Unlock()

	if compareVersions(st.f.versions[node], "v1.9") < 0 {
		return nil, status.Error(codes.Unimplemented, "unknown service storage.StorageService")
	}

	if st.f.wipeErr != nil {
		return nil, st.f.wipeErr
	}

	st.f.wipes = append(st.f.wipes, req)

	return &storageapi.BlockDeviceWipeResponse{Messages: []*storageapi.BlockDeviceWipe{{}}}, nil
}

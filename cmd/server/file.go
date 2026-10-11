package main

import (
	fileHandler "github.com/viettechno/vietgara-backend/internal/file/handler"
	fileRepository "github.com/viettechno/vietgara-backend/internal/file/repository"
	fileUsecase "github.com/viettechno/vietgara-backend/internal/file/usecase"
	platformdb "github.com/viettechno/vietgara-backend/internal/platform/db"
	"github.com/viettechno/vietgara-backend/internal/platform/storage"
	tenantRepo "github.com/viettechno/vietgara-backend/internal/tenant/repository"
)

// fileModule is the wired file registry (Release 5.0): uploaded files with
// their owner, kind and object key, over the object store.
type fileModule struct {
	server *fileHandler.Server
	uc     *fileUsecase.UseCase
}

func buildFiles(pool *platformdb.Pool, store storage.Store, garages *tenantRepo.PostgresGarageRepository) *fileModule {
	uc := fileUsecase.New(fileRepository.NewPostgresRepository(pool), store, fileUsage{garages: garages})

	return &fileModule{server: fileHandler.NewServer(uc), uc: uc}
}

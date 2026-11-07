package repository

import (
	"database/sql"
	"errors"

	"github.com/axadrn/moin/controlplane/internal/model"
	"github.com/jmoiron/sqlx"
)

var (
	ErrFileNotFound = errors.New("file not found")
)

// Files stores the metadata of uploaded files.
type Files struct {
	db *sqlx.DB
}

func NewFiles(db *sqlx.DB) *Files {
	return &Files{db: db}
}

func (r *Files) Create(file *model.File) error {
	query := `INSERT INTO files (id, user_id, owner_type, owner_id, type, filename, original_name, mime_type, size, storage_path, public, created_at)
	          VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

	_, err := r.db.Exec(query,
		file.ID,
		file.UserID,
		file.OwnerType,
		file.OwnerID,
		file.Type,
		file.Filename,
		file.OriginalName,
		file.MimeType,
		file.Size,
		file.StoragePath,
		file.Public,
		file.CreatedAt,
	)

	return err
}

func (r *Files) ByID(id string) (*model.File, error) {
	file := &model.File{}
	query := `SELECT * FROM files WHERE id = $1`

	err := r.db.Get(file, query, id)
	if err == sql.ErrNoRows {
		return nil, ErrFileNotFound
	}

	return file, err
}

func (r *Files) FileByType(ownerType, ownerID, fileType string) (*model.File, error) {
	file := &model.File{}
	query := `SELECT * FROM files WHERE owner_type = $1 AND owner_id = $2 AND type = $3 ORDER BY created_at DESC LIMIT 1`

	err := r.db.Get(file, query, ownerType, ownerID, fileType)
	if err == sql.ErrNoRows {
		return nil, ErrFileNotFound
	}

	return file, err
}

func (r *Files) Files(ownerType, ownerID string) ([]*model.File, error) {
	var files []*model.File
	query := `SELECT * FROM files WHERE owner_type = $1 AND owner_id = $2`

	err := r.db.Select(&files, query, ownerType, ownerID)
	if err != nil {
		return nil, err
	}

	return files, nil
}

func (r *Files) AllUserFiles(userID string) ([]*model.File, error) {
	var files []*model.File
	query := `SELECT * FROM files WHERE user_id = $1 ORDER BY created_at DESC`

	err := r.db.Select(&files, query, userID)
	if err != nil {
		return nil, err
	}

	return files, nil
}

func (r *Files) Delete(id string) error {
	query := `DELETE FROM files WHERE id = $1`
	_, err := r.db.Exec(query, id)
	return err
}

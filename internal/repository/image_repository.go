package repository

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"image-thumbnail-api/internal/model"
)

const imagesCollection = "images"

// ErrNotFound is returned when an image document does not exist.
var ErrNotFound = errors.New("image not found")

// ImageRepository persists Image documents (with embedded thumbnails) in MongoDB.
type ImageRepository struct {
	images *mongo.Collection
}

// NewImageRepository creates a repository backed by the given database.
func NewImageRepository(db *mongo.Database) *ImageRepository {
	return &ImageRepository{
		images: db.Collection(imagesCollection),
	}
}

// EnsureIndexes ensures indexes for the images collection.
// Image.ID is mapped to MongoDB _id, which already has a unique index,
// so creating another index on ID is not appropriate.
func (r *ImageRepository) EnsureIndexes(ctx context.Context) error {
	return nil
}

// CreateImage inserts a new Image document.
func (r *ImageRepository) CreateImage(ctx context.Context, img *model.Image) error {
	_, err := r.images.InsertOne(ctx, img)
	if err != nil {
		return fmt.Errorf("insert image: %w", err)
	}
	return nil
}

// GetImageByID loads an Image document by its ID.
// Returns ErrNotFound when no document matches.
func (r *ImageRepository) GetImageByID(ctx context.Context, id string) (*model.Image, error) {
	var img model.Image
	err := r.images.FindOne(ctx, bson.M{"_id": id}).Decode(&img)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find image: %w", err)
	}
	return &img, nil
}

// DeleteImage removes an Image document by its ID.
// Returns ErrNotFound when no document matches.
func (r *ImageRepository) DeleteImage(ctx context.Context, id string) error {
	res, err := r.images.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("delete image: %w", err)
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}

// GetImageByThumbnailID finds the Image document that embeds the given thumbnail ID.
// Returns ErrNotFound when no document matches.
func (r *ImageRepository) GetImageByThumbnailID(ctx context.Context, thumbnailID string) (*model.Image, error) {
	var img model.Image
	err := r.images.FindOne(ctx, bson.M{"thumbnails.id": thumbnailID}).Decode(&img)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("find image by thumbnail id: %w", err)
	}
	return &img, nil
}

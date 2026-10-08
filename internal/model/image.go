package model

import "time"

// Image is the MongoDB document for an uploaded original image
// and the thumbnails generated from it.
type Image struct {
	ID               string      `bson:"_id" json:"id"`
	OriginalFilename string      `bson:"original_filename" json:"original_filename"`
	ContentType      string      `bson:"content_type" json:"content_type"`
	OriginalWidth    int         `bson:"original_width" json:"original_width"`
	OriginalHeight   int         `bson:"original_height" json:"original_height"`
	OriginalSize     int64       `bson:"original_size" json:"original_size"`
	OriginalPath     string      `bson:"original_path" json:"original_path"`
	CreatedAt        time.Time   `bson:"created_at" json:"created_at"`
	Thumbnails       []Thumbnail `bson:"thumbnails" json:"thumbnails"`
}

// Thumbnail is metadata for one generated resized image.
// It is embedded on Image and can also be queried on its own later.
type Thumbnail struct {
	ID              string    `bson:"id" json:"id"`
	ImageID         string    `bson:"image_id" json:"image_id"`
	Preset          string    `bson:"preset" json:"preset"` // small medium large  custom
	RequestedWidth  int       `bson:"requested_width" json:"requested_width"`
	RequestedHeight int       `bson:"requested_height" json:"requested_height"`
	ActualWidth     int       `bson:"actual_width" json:"actual_width"`
	ActualHeight    int       `bson:"actual_height" json:"actual_height"`
	ContentType     string    `bson:"content_type" json:"content_type"`
	FileSize        int64     `bson:"file_size" json:"file_size"`
	FilePath        string    `bson:"file_path" json:"file_path"`
	CreatedAt       time.Time `bson:"created_at" json:"created_at"`
}

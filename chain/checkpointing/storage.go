package checkpointing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func StoreConfig(ctx context.Context, host, accessKeyId, secretAccessKey, bucketName, hash string) error {
	// Initialize minio client object.
	minioClient, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyId, secretAccessKey, ""),
		Secure: false,
	})
	if err != nil {
		return err
	}

	filename := hash+".txt"
    filePath := "/tmp/"+filename
    contentType := "text/plain"

	_, err = minioClient.FPutObject(ctx, bucketName, filename, filePath, minio.PutObjectOptions{ContentType: contentType})
    if err != nil {
        return err
    }

	return nil
}

func CreateConfig(data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)

	err := os.WriteFile("/tmp/"+hex.EncodeToString(hash[:])+".txt", data, 0644)
	if err != nil {
		return nil, err
	}

	return hash[:], nil
}

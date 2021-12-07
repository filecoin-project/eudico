package checkpointing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func StoreConfig() {
	// Initialize minio client object.
	minioClient, err := minio.New(S3_HOST, &minio.Options{
		Creds:  credentials.NewStaticV4(ACCESS_KEY_ID, SECRET_ACCESS_KEY, ""),
		Secure: false,
	})
	if err != nil {
		panic(err)
	}

	fmt.Println(minioClient)
}

func CreateConfig(data []byte) ([]byte, error) {
	hash := sha256.Sum256(data)

	fmt.Println(hex.EncodeToString(hash[:]))
	err := os.WriteFile("/tmp/"+hex.EncodeToString(hash[:]), data, 0644)
	if err != nil {
		return nil, err
	}

	return hash[:], nil
}

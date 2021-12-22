package checkpointing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"strings"

	"github.com/minio/minio-go/v7"
)

func StoreConfig(ctx context.Context, minioClient *minio.Client, bucketName, hash string) error {
	filename := hash + ".txt"
	filePath := "/tmp/" + filename
	contentType := "text/plain"

	_, err := minioClient.FPutObject(ctx, bucketName, filename, filePath, minio.PutObjectOptions{ContentType: contentType})
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

func GetConfig(ctx context.Context, minioClient *minio.Client, bucketName, hash string) (string, error) {
	filename := hash + ".txt"
	filePath := "/tmp/verification/" + filename

	err := minioClient.FGetObject(context.Background(), bucketName, filename, filePath, minio.GetObjectOptions{})
	if err != nil {
		return "", err
	}

	content, err := ioutil.ReadFile(filePath) // the file is inside the local directory
	if err != nil {
		return "", err
	}
	cpCid := strings.Split(string(content), "\n")[0]
	fmt.Println(cpCid)

	return cpCid, nil
}

package vod

import (
	"context"
	"fmt"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/vod/v20180717"
	"github.com/tencentyun/cos-go-sdk-v5"
	"net/http"
	"net/url"
	"os"
	"time"
)

const multipartUploadThreshold = 5 * 1024 * 1024

type VodUploadClient struct {
	SecretId  string
	SecretKey string
	Timeout   int64
}

func (p *VodUploadClient) Upload(region string, request *VodUploadRequest) (*VodUploadResponse, error) {
	if err := p.prefixCheckAndSetDefaultVal(region, request); err != nil {
		return nil, err
	}

	credential := common.NewCredential(p.SecretId, p.SecretKey)
	prof := profile.NewClientProfile()
	apiClient, err := v20180717.NewClient(credential, region, prof)
	if err != nil {
		return nil, err
	}

	applyUploadResponse, err := apiClient.ApplyUpload(&request.ApplyUploadRequest)
	if err != nil {
		return nil, err
	}

	cosTransport := cos.AuthorizationTransport{}
	tempCertificate := applyUploadResponse.Response.TempCertificate
	if tempCertificate == nil {
		cosTransport.SecretID = p.SecretId
		cosTransport.SecretKey = p.SecretKey
	} else {
		cosTransport.SecretID = *tempCertificate.SecretId
		cosTransport.SecretKey = *tempCertificate.SecretKey
		cosTransport.SessionToken = *tempCertificate.Token
	}

	var timeout int64
	if p.Timeout > 0 {
		timeout = p.Timeout
	} else {
		timeout = 30
	}

	hostUrl := fmt.Sprintf("https://%s.cos.%s.myqcloud.com", *applyUploadResponse.Response.StorageBucket, *applyUploadResponse.Response.StorageRegion)
	u, _ := url.Parse(hostUrl)
	cosUrl := &cos.BaseURL{BucketURL: u}
	cosClient := cos.NewClient(cosUrl, &http.Client{
		Timeout:   time.Duration(timeout) * time.Second,
		Transport: &cosTransport,
	})

	mediaStoragePath := applyUploadResponse.Response.MediaStoragePath
	if NotEmptyStr(request.MediaType) && NotEmptyStr(mediaStoragePath) {
		if err = p.uploadCos(cosClient, *request.MediaFilePath, (*mediaStoragePath)[1:]); err != nil {
			return nil, err
		}
	}

	coverStoragePath := applyUploadResponse.Response.CoverStoragePath
	if NotEmptyStr(request.CoverType) && NotEmptyStr(coverStoragePath) {
		if err = p.uploadCos(cosClient, *request.CoverFilePath, (*coverStoragePath)[1:]); err != nil {
			return nil, err
		}
	}

	commitUploadRequest := v20180717.NewCommitUploadRequest()
	commitUploadRequest.SubAppId = request.SubAppId
	commitUploadRequest.VodSessionKey = applyUploadResponse.Response.VodSessionKey
	commitUploadResponse, err := apiClient.CommitUpload(commitUploadRequest)
	if err != nil {
		return nil, err
	}
	vodUploadResponse := &VodUploadResponse{
		CommitUploadResponse: *commitUploadResponse,
	}

	return vodUploadResponse, nil
}

func (p *VodUploadClient) uploadCos(client *cos.Client, localPath string, cosPath string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	stat, err := file.Stat()
	if err != nil {
		return err
	}

	if stat.Size() < multipartUploadThreshold {
		putOpt := &cos.ObjectPutOptions{
			ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
				ContentLength: int(stat.Size()),
			},
		}
		_, err = client.Object.Put(context.Background(), cosPath, file, putOpt)
		if err != nil {
			return err
		}
	} else {
		multiOpt := &cos.MultiUploadOptions{
			OptIni:   nil,
			PartSize: 1,
		}
		_, _, err = client.Object.MultiUpload(context.Background(), cosPath, localPath, multiOpt)
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *VodUploadClient) prefixCheckAndSetDefaultVal(region string, request *VodUploadRequest) error {
	if region == "" {
		return &VodClientError{
			Message: "lack region",
		}
	}
	if IsEmptyStr(request.MediaFilePath) {
		return &VodClientError{
			Message: "lack media path",
		}
	}
	if !FileExist(*request.MediaFilePath) {
		return &VodClientError{
			Message: "media path is invalid",
		}
	}
	if IsEmptyStr(request.MediaType) {
		mediaType := GetFileType(*request.MediaFilePath)
		if mediaType == "" {
			return &VodClientError{
				Message: "lack media type",
			}
		}
		request.MediaType = &mediaType
	}
	if IsEmptyStr(request.MediaName) {
		mediaName := GetFileName(*request.MediaFilePath)
		request.MediaName = &mediaName
	}

	if NotEmptyStr(request.CoverFilePath) {
		if !FileExist(*request.CoverFilePath) {
			return &VodClientError{
				Message: "cover path is invalid",
			}
		}
		if IsEmptyStr(request.CoverType) {
			coverType := GetFileType(*request.CoverFilePath)
			if coverType == "" {
				return &VodClientError{
					Message: "lack cover type",
				}
			}
			request.CoverType = &coverType
		}
	}

	return nil
}

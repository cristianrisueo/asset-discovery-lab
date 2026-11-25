package labkit

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3 is a client for RustFS through the S3 API, with path-style addressing.
type S3 struct {
	endpoint string
	cl       *s3.Client
}

// NewS3 builds a client. It does not connect until the first call.
func NewS3(endpoint, accessKey, secretKey, region string) *S3 {
	cl := s3.New(s3.Options{
		Region:                     region,
		BaseEndpoint:               aws.String(endpoint),
		UsePathStyle:               true,
		Credentials:                credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
		HTTPClient:                 httpClient,
	})
	return &S3{endpoint: endpoint, cl: cl}
}

// ListKeys lists every object key under prefix in bucket.
func (c *S3) ListKeys(ctx context.Context, bucket, prefix string) ([]string, error) {
	var keys []string
	p := s3.NewListObjectsV2Paginator(c.cl, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(prefix)})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3 at %s: list s3://%s/%s: %w", c.endpoint, bucket, prefix, err)
		}
		for _, o := range page.Contents {
			keys = append(keys, aws.ToString(o.Key))
		}
	}
	return keys, nil
}

// ParseS3URI splits s3://bucket/some/prefix into the bucket and a prefix
// without a leading slash.
func ParseS3URI(uri string) (bucket, prefix string, err error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", "", err
	}
	if (u.Scheme != "s3" && u.Scheme != "s3a") || u.Host == "" {
		return "", "", fmt.Errorf("not an s3 URI: %q", uri)
	}
	return u.Host, strings.TrimPrefix(u.Path, "/"), nil
}

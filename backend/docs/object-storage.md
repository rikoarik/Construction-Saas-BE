# Backend Object Storage

Configure an S3-compatible private bucket. The backend adapter never sets public ACLs; production buckets must also use private bucket policy and encryption at rest. Application services must perform tenant/resource authorization before calls that expose downloads or mutate object ownership.

Required adapter settings:

- `S3_BUCKET`: private bucket name
- `S3_PREFIX`: optional namespace prefix
- `S3_MAX_OBJECT_BYTES`: upload limit
- `S3_DOWNLOAD_TTL`: signed URL lifetime, at most 15 minutes
- AWS SDK endpoint/credentials/region: configured by the deployment environment, including MinIO-compatible endpoints

`internal/objectstore.Store` supports bounded uploads, deletes, and signed downloads. The current integration package has unit tests using a fake HTTP transport. The worker has no default object-store handler; domain/application services should inject the adapter through a port.

Local MinIO can be added to `docker compose up -d minio` when Docker is available. Create a private bucket before testing against it. Do not expose the MinIO console or root credentials in a deployed environment.

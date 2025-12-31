#!/bin/bash
set -e

API_URL="http://localhost:8080"
JOB_ID=8

echo "1. Request presigned upload URL..."
RESPONSE=$(curl -s -X POST ${API_URL}/artifacts/upload-url \
  -H "Content-Type: application/json" \
  -d "{\"job_id\": ${JOB_ID}, \"filename\": \"artifacts.zip\"}")

echo $RESPONSE | jq .

UPLOAD_URL=$(echo $RESPONSE | jq -r .upload_url)
OBJECT_KEY=$(echo $RESPONSE | jq -r .object_key)

echo ""
echo "2. Upload file to MinIO..."
curl -X PUT "${UPLOAD_URL}" \
  -H "Content-Type: application/zip" \
  --data-binary @artifacts.zip

echo ""
echo "3. Mark upload complete..."
FILE_SIZE=$(wc -c < artifacts.zip | tr -d ' ')
curl -X POST ${API_URL}/artifacts/complete \
  -H "Content-Type: application/json" \
  -d "{
    \"job_id\": ${JOB_ID},
    \"filename\": \"artifacts.zip\",
    \"object_key\": \"${OBJECT_KEY}\",
    \"size_bytes\": ${FILE_SIZE},
    \"content_type\": \"application/zip\"
  }"

echo ""
echo "4. Get artifact ID from database..."
ARTIFACT_ID=$(psql "postgres://postgres:postgres@localhost:5432/cicd?sslmode=disable" -t -c "
SELECT id FROM artifacts WHERE job_id = ${JOB_ID} ORDER BY id DESC LIMIT 1;
" | tr -d ' ')

echo "Artifact ID: ${ARTIFACT_ID}"

echo ""
echo "5. Request presigned download URL..."
DOWNLOAD_RESPONSE=$(curl -s ${API_URL}/artifacts/${ARTIFACT_ID}/download)
echo $DOWNLOAD_RESPONSE | jq .

DOWNLOAD_URL=$(echo $DOWNLOAD_RESPONSE | jq -r .download_url)

echo ""
echo "6. Download file from MinIO..."
curl -o downloaded-artifacts.zip "${DOWNLOAD_URL}"

echo ""
echo "7. Verify downloaded file..."
unzip -l downloaded-artifacts.zip
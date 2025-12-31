#!/bin/bash
set -e

API_URL="http://localhost:8080"

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${BLUE}=== MinIO Artifact Upload/Download Test ===${NC}\n"

# Get the latest job
echo "Finding a valid job..."
JOB_ID=$(psql "postgres://postgres:postgres@localhost:5432/cicd?sslmode=disable" -t -c "
SELECT id FROM jobs WHERE status = 'success' ORDER BY id DESC LIMIT 1;
" | tr -d ' ')

if [ -z "$JOB_ID" ]; then
    echo -e "${RED}✗ No successful jobs found!${NC}"
    echo "Please trigger a workflow first:"
    echo "  curl -X POST http://localhost:8080/runs/trigger \\"
    echo "    -H 'Content-Type: application/json' \\"
    echo "    -d '{\"repo_path\": \"control-plane/examples/repo\", \"workflow_path\": \".ci/workflows/build.yml\"}'"
    exit 1
fi

echo -e "${GREEN}✓ Using Job ID: $JOB_ID${NC}\n"

# Check if artifacts.zip exists
if [ ! -f "artifacts.zip" ]; then
    echo "Creating test artifact..."
    mkdir -p repo/dist
    echo "Build output - version 1.0.0" > repo/dist/app.txt
    echo "console.log('Hello CI/CD')" > repo/dist/app.js
    cd repo && zip -r ../artifacts.zip dist/ && cd ..
    echo -e "${GREEN}✓ Created artifacts.zip${NC}\n"
fi

echo -e "${BLUE}Step 1: Request presigned upload URL${NC}"
RESPONSE=$(curl -s -X POST ${API_URL}/artifacts/upload-url \
  -H "Content-Type: application/json" \
  -d "{\"job_id\": ${JOB_ID}, \"filename\": \"artifacts.zip\"}")

echo "$RESPONSE" | jq .

UPLOAD_URL=$(echo $RESPONSE | jq -r .upload_url)
OBJECT_KEY=$(echo $RESPONSE | jq -r .object_key)

if [ "$UPLOAD_URL" = "null" ]; then
    echo -e "${RED}✗ Failed to get upload URL${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Got presigned URL${NC}\n"

echo -e "${BLUE}Step 2: Upload file to MinIO${NC}"
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X PUT "${UPLOAD_URL}" \
  -H "Content-Type: application/zip" \
  --data-binary @artifacts.zip)

if [ "$HTTP_CODE" = "200" ]; then
    echo -e "${GREEN}✓ Upload successful (HTTP $HTTP_CODE)${NC}\n"
else
    echo -e "${RED}✗ Upload failed (HTTP $HTTP_CODE)${NC}"
    exit 1
fi

echo -e "${BLUE}Step 3: Mark upload complete${NC}"
FILE_SIZE=$(wc -c < artifacts.zip | tr -d ' ')
COMPLETE_RESPONSE=$(curl -s -X POST ${API_URL}/artifacts/complete \
  -H "Content-Type: application/json" \
  -d "{
    \"job_id\": ${JOB_ID},
    \"filename\": \"artifacts.zip\",
    \"object_key\": \"${OBJECT_KEY}\",
    \"size_bytes\": ${FILE_SIZE},
    \"content_type\": \"application/zip\"
  }")

echo "$COMPLETE_RESPONSE" | jq .

ARTIFACT_ID=$(echo "$COMPLETE_RESPONSE" | jq -r .artifact_id)

if [ "$ARTIFACT_ID" = "null" ] || [ -z "$ARTIFACT_ID" ]; then
    echo -e "${RED}✗ Failed to save metadata${NC}"
    echo "Response: $COMPLETE_RESPONSE"
    exit 1
fi

echo -e "${GREEN}✓ Metadata saved (Artifact ID: $ARTIFACT_ID)${NC}\n"

echo -e "${BLUE}Step 4: Request presigned download URL${NC}"
DOWNLOAD_RESPONSE=$(curl -s ${API_URL}/artifacts/${ARTIFACT_ID}/download)
echo "$DOWNLOAD_RESPONSE" | jq .

DOWNLOAD_URL=$(echo $DOWNLOAD_RESPONSE | jq -r .download_url)

if [ "$DOWNLOAD_URL" = "null" ]; then
    echo -e "${RED}✗ Failed to get download URL${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Got download URL${NC}\n"

echo -e "${BLUE}Step 5: Download file from MinIO${NC}"
curl -s -o downloaded-artifacts.zip "${DOWNLOAD_URL}"

if [ ! -f "downloaded-artifacts.zip" ]; then
    echo -e "${RED}✗ Download failed${NC}"
    exit 1
fi

echo -e "${GREEN}✓ Downloaded${NC}\n"

echo -e "${BLUE}Step 6: Verify downloaded file${NC}"
ORIGINAL_SIZE=$(wc -c < artifacts.zip | tr -d ' ')
DOWNLOADED_SIZE=$(wc -c < downloaded-artifacts.zip | tr -d ' ')

echo "Original size: $ORIGINAL_SIZE bytes"
echo "Downloaded size: $DOWNLOADED_SIZE bytes"

if [ "$ORIGINAL_SIZE" = "$DOWNLOADED_SIZE" ]; then
    echo -e "${GREEN}✓ Sizes match!${NC}\n"
    echo "Contents:"
    unzip -l downloaded-artifacts.zip
    echo -e "\n${GREEN}=== TEST PASSED ===${NC}\n"
    
    # Verify in database
    echo "Artifact in database:"
    psql "postgres://postgres:postgres@localhost:5432/cicd?sslmode=disable" -c "
    SELECT id, job_id, name, size_bytes, created_at 
    FROM artifacts 
    WHERE id = ${ARTIFACT_ID};
    "
    
    rm -f downloaded-artifacts.zip
else
    echo -e "${RED}✗ Size mismatch!${NC}"
    exit 1
fi
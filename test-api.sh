#!/bin/bash

echo "=== Testing CI/CD Platform API ==="

echo -e "\n1. Health Check:"
curl -s http://localhost/api/health

echo -e "\n\n2. List Runs:"
curl -s http://localhost/api/runs

echo -e "\n\nGet Specific Job6:"
curl -s http://localhost/api/runs/6

echo -e "\n\n3. List Jobs:"
curl -s http://localhost/api/jobs | head -c 500

echo -e "\n\n4. Dashboard (HTML check):"
curl -s http://localhost/ | head -c 200

echo -e "\n\n5. Static Assets:"
curl -sI http://localhost/static/js/main.*.js | head -5

echo -e "\n\n=== All tests complete ==="
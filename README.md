# Go CRUD API with PostgreSQL

Building HTTP and JSON APIs with Go. You can see the responses with curl.

## List all items

curl -s http://localhost:3000/items | jq

## Get a single item by ID

curl -s http://localhost:3000/items/1 | jq

## Create a new item (requires auth)

curl -i -X POST http://localhost:3000/items \
  -H "Authorization: Bearer secret-token" \
  -H "Content-Type: application/json" \
  -d '{"name":"New Item"}'


## Update an item (requires auth)

curl -i -X PUT http://localhost:3000/items/1 \
  -H "Authorization: Bearer secret-token" \
  -H "Content-Type: application/json" \
  -d '{"name":"Updated Name"}'

## Delete an item (requires auth)  

curl -i -X DELETE http://localhost:3000/items/1 \
  -H "Authorization: Bearer secret-token"

  

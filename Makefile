run:
	go run cmd/main.go
push:
	git add . && git commit -m "$(m)" && git push
docker:
	#Pass version using v variable
	sudo docker build  --platform linux/amd64 -t dafraer/deepseekbot:$(v) .
	docker push dafraer/deepseekbot:$(v)
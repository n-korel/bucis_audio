.PHONY: build bucis brs brs-windows clean lint run-bucis run-bucis-mic run-brs

build: bucis brs

./bin:
	mkdir -p "./bin"

bucis: ./bin
	go build -o "./bin/bucis" ./cmd/bucis

brs: ./bin
	go build -o "./bin/brs" ./cmd/brs

brs-windows: ./bin
	GOOS=windows GOARCH=amd64 go build -o "./bin/brs.exe" ./cmd/brs

run-bucis: bucis
	set -a; [ -f .env ] && . ./.env; set +a; "./bin/bucis"

run-bucis-mic: bucis
	set -a; [ -f .env ] && . ./.env; set +a; "./bin/bucis" --sound-type 2

run-brs: brs
	set -a; [ -f .env ] && . ./.env; set +a; "./bin/brs"

clean:
	rm -rf "./bin"

lint:
	golangci-lint run ./...

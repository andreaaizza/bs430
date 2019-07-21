TRG = bs430
SRC = $(shell find . -type f -name '*.go')

BUILD_GOPATH = ${GOPATH}:$(CURDIR)

.PHONY: run clean rebuild getver

build: getver bin/${TRG} ${SRC}
bin/${TRG}: ${SRC} 
	@echo depends on ${SRC}
	GOPATH="${BUILD_GOPATH}" go install -ldflags "-X version.Version=${VER}" ${TRG}
	GOARCH=arm GOPATH="${BUILD_GOPATH}" go install -ldflags "-X version.Version=${VER}" ${TRG}
getver: 
	$(eval VER=$(shell git rev-parse --short HEAD))
	$(if $(shell git status --porcelain -uno), $(eval VER=${VER}-DIRTY),)
	@echo Version is ${VER}
run: build
	$(if ${MY_BS430}, $(shell sudo ./bin/${TRG} ${MY_BS430}), @echo "Cannot run. Please set MY_BS430 to your BS430 BT MAC addr e.g. 'MY_BS430=AA:BB:CC:DD:FF:00 make run'")
clean: 
	@echo Cleaning ${TRG} from \'bin\' directory
	$(shell find bin -type f -name ${TRG} -exec rm {} \;)
rebuild: clean build

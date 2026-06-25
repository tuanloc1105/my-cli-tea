UNAME_S := $(shell uname -s)
DEV_KIT_LOCATION ?= D:/dev-kit

ifneq (,$(findstring MINGW,$(UNAME_S))$(findstring MSYS,$(UNAME_S)))
    GOOS       = windows
    GOARCH     = amd64
    EXT        = .exe
    INSTALL_DIR = $(DEV_KIT_LOCATION)/tool
    INSTALL     = mv -f
    CASE_NAME   = case-converter
    CLEAN_CMD   = rm -f
    export TEMP := $(shell cygpath -w /tmp)
    export GOPATH := $(HOME)/go
    export GOCACHE := $(HOME)/go/cache
else ifeq ($(UNAME_S),Darwin)
    GOOS       = darwin
    GOARCH     =
    EXT        =
    INSTALL_DIR = $(HOME)/dev-kit/tool
    INSTALL     = mv -f
    CASE_NAME   = c
    CLEAN_CMD   = rm -f
else
    GOOS       = linux
    GOARCH     =
    EXT        =
    INSTALL_DIR = /usr/local/bin
    INSTALL     = sudo mv
    CASE_NAME   = c
    CLEAN_CMD   = rm -f
endif

GOBUILD = CGO_ENABLED=0 GOOS=$(GOOS) $(if $(GOARCH),GOARCH=$(GOARCH)) go build -o

.PHONY: all case-converter check-folder-size find-content find-everything replace-text api-stress-test clean

all: case-converter check-folder-size find-content find-everything replace-text api-stress-test

case-converter:
	cd case-converter && $(GOBUILD) case-converter$(EXT) .
	$(INSTALL) case-converter/case-converter$(EXT) $(INSTALL_DIR)/$(CASE_NAME)$(EXT)

check-folder-size:
	cd check-folder-size && $(GOBUILD) check-folder-size$(EXT) .
	$(INSTALL) check-folder-size/check-folder-size$(EXT) $(INSTALL_DIR)/check-folder-size$(EXT)

find-content:
	cd find-content && $(GOBUILD) find-content$(EXT) .
	$(INSTALL) find-content/find-content$(EXT) $(INSTALL_DIR)/find-content$(EXT)

find-everything:
	cd find-everything && $(GOBUILD) find-everything$(EXT) .
	$(INSTALL) find-everything/find-everything$(EXT) $(INSTALL_DIR)/find-everything$(EXT)

replace-text:
	cd replace-text && $(GOBUILD) replace-text$(EXT) .
	$(INSTALL) replace-text/replace-text$(EXT) $(INSTALL_DIR)/replace-text$(EXT)

api-stress-test:
	cd api-stress-test && $(GOBUILD) api-stress-test$(EXT) .
	$(INSTALL) api-stress-test/api-stress-test$(EXT) $(INSTALL_DIR)/api-stress-test$(EXT)

clean:
	$(CLEAN_CMD) */case-converter$(EXT) */check-folder-size$(EXT) */find-content$(EXT) */find-everything$(EXT) */replace-text$(EXT) */api-stress-test$(EXT)

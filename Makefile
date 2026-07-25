.PHONY: all build deps clean run

all: build

build:
	go build -o muzak321 .

portaudio_local := $(HOME)/.local
portaudio_pc := $(portaudio_local)/lib/pkgconfig/portaudio-2.0.pc
export LD_LIBRARY_PATH := $(portaudio_local)/lib

$(portaudio_pc): /tmp/pa-local/usr/lib/x86_64-linux-gnu/pkgconfig/portaudio-2.0.pc
	mkdir -p $(portaudio_local)/lib/pkgconfig $(portaudio_local)/include
	cp /tmp/pa-local/usr/lib/x86_64-linux-gnu/pkgconfig/portaudio-2.0.pc $(portaudio_pc)
	cp /tmp/pa-local/usr/include/portaudio.h $(portaudio_local)/include/
	cp /tmp/pa-local/usr/lib/x86_64-linux-gnu/libportaudio.so* $(portaudio_local)/lib/
	ln -sf /lib/x86_64-linux-gnu/libasound.so.2 $(portaudio_local)/lib/libasound.so

deps: $(portaudio_pc)

build_local: deps
	PKG_CONFIG_PATH="$(portaudio_local)/lib/pkgconfig" \
	CGO_LDFLAGS="-L$(portaudio_local)/lib" \
	CGO_CFLAGS="-I$(portaudio_local)/include -pthread" \
	go build -o muzak321 .

run: build_local
	muzak321

clean:
	rm -f muzak321

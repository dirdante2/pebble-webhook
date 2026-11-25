#!/bin/bash
# Baut die lokale Transkription (whisper.cpp-Server + schlankes ffmpeg) statisch für die Architektur dieses Rechners
# und legt sie gepackt unter internal/whisper/engine/<arch>/ ab, wo go build sie einbettet (nicht in Git).
#   deploy/whisper-engine.sh            baut nur, wenn die Dateien zu den Versionen unten fehlen
#   deploy/whisper-engine.sh --neu      baut immer
# Release-Workflow: läuft je Architektur in Alpine (musl, voll statisch); lokal reicht glibc mit -static.
# Benötigt: gcc/g++, make, cmake, curl, xz.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

WHISPER_VERSION=1.9.6
WHISPER_SHA256=7ab15dc88022d2425ad94dcffd8abbf1b6d7018089b4e48d6b40780738e0b445
FFMPEG_VERSION=8.1.3
FFMPEG_SHA256=7138d28c96d9d3e3af4ee3d8cad72741f8ffb40da90c1112235dea3ecd3178a3
STAND="whisper.cpp $WHISPER_VERSION (server), ffmpeg $FFMPEG_VERSION"

case "$(uname -m)" in
    x86_64) ARCH=amd64 ;;
    aarch64|arm64) ARCH=arm64 ;;
    *) echo "Architektur $(uname -m) wird nicht unterstützt" >&2; exit 1 ;;
esac
ZIEL="internal/whisper/engine/$ARCH"
if [ "${1:-}" != "--neu" ] && [ "$(cat "$ZIEL/STAND" 2>/dev/null)" = "$STAND" ]; then
    echo "Whisper-Engine $ARCH aktuell ($STAND)"
    exit 0
fi
command -v cmake >/dev/null || PATH="$HOME/.local/bin:$PATH"
command -v cmake >/dev/null || { echo "cmake fehlt (z. B. pip install --user cmake)" >&2; exit 1; }

BAU="${ENGINE_BUILD_DIR:-$PWD/lokal/engine-build}/$ARCH"
mkdir -p "$BAU"
JOBS="$(nproc)"

hole() { # url datei sha256
    [ -f "$BAU/$2" ] && echo "$3  $BAU/$2" | sha256sum -c --quiet 2>/dev/null && return
    curl -fsSL -o "$BAU/$2" "$1"
    echo "$3  $BAU/$2" | sha256sum -c --quiet || { echo "Prüfsumme von $2 falsch" >&2; exit 1; }
}
hole "https://ffmpeg.org/releases/ffmpeg-$FFMPEG_VERSION.tar.xz" "ffmpeg-$FFMPEG_VERSION.tar.xz" "$FFMPEG_SHA256"
hole "https://github.com/ggml-org/whisper.cpp/archive/refs/tags/v$WHISPER_VERSION.tar.gz" "whisper-$WHISPER_VERSION.tar.gz" "$WHISPER_SHA256"

# ffmpeg: nur Lesen der Aufnahme-Formate (m4a/AAC, mp3, ogg/Opus/Vorbis, webm, wav, flac) und Schreiben von 16-kHz-WAV.
FF="$BAU/ffmpeg-$FFMPEG_VERSION"
rm -rf "$FF" && tar -xJf "$BAU/ffmpeg-$FFMPEG_VERSION.tar.xz" -C "$BAU"
(cd "$FF" && ./configure --prefix="$FF/out" --enable-static --disable-shared --disable-autodetect --disable-everything \
    --disable-doc --disable-network --disable-debug --disable-x86asm --enable-small \
    --disable-programs --enable-ffmpeg --disable-avdevice --disable-swscale \
    --enable-avformat --enable-avcodec --enable-swresample --enable-avfilter \
    --enable-protocol=file,pipe --enable-demuxer=mov,mp3,ogg,wav,matroska,aac,flac \
    --enable-decoder=aac,aac_fixed,mp3,mp3float,vorbis,opus,flac,pcm_s16le,pcm_f32le \
    --enable-parser=aac,mpegaudio,vorbis,opus,flac --enable-muxer=wav --enable-encoder=pcm_s16le \
    --enable-filter=aresample,aformat,anull,abuffer,abuffersink \
    --extra-ldflags=-static --pkg-config-flags=--static >"$BAU/ffmpeg-configure.log" && make -s -j"$JOBS" >"$BAU/ffmpeg-make.log" 2>&1) || { echo "ffmpeg-Bau gescheitert, siehe $BAU/ffmpeg-*.log" >&2; exit 1; }

# whisper.cpp: whisper-server statisch (hält das Modell im RAM), ohne OpenMP (pthreads reichen). x86: drei Stufen, die App wählt nach CPU.
WH="$BAU/whisper.cpp-$WHISPER_VERSION"
rm -rf "$WH" && tar -xzf "$BAU/whisper-$WHISPER_VERSION.tar.gz" -C "$BAU"
baue() { # name cmake-optionen…
    local name="$1"; shift
    cmake -S "$WH" -B "$WH/bau-$name" -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF -DGGML_NATIVE=OFF \
        -DGGML_OPENMP=OFF -DGGML_CCACHE=OFF -DWHISPER_BUILD_TESTS=OFF -DWHISPER_BUILD_EXAMPLES=ON -DWHISPER_BUILD_SERVER=ON -DWHISPER_SDL2=OFF \
        -DCMAKE_EXE_LINKER_FLAGS=-static "$@" >/dev/null
    cmake --build "$WH/bau-$name" --target whisper-server -j"$JOBS" >/dev/null
    cp "$WH/bau-$name/bin/whisper-server" "$BAU/whisper-server-$name"
}
if [ "$ARCH" = amd64 ]; then
    baue basis -DGGML_AVX=OFF -DGGML_AVX2=OFF -DGGML_FMA=OFF -DGGML_F16C=OFF -DGGML_BMI2=OFF
    baue avx2
    baue avx512 -DGGML_AVX512=ON
    VARIANTEN="basis avx2 avx512"
else
    baue basis
    VARIANTEN="basis"
fi

rm -rf "$ZIEL" && mkdir -p "$ZIEL"
strip "$FF/ffmpeg" -o "$BAU/ffmpeg"
gzip -9c "$BAU/ffmpeg" > "$ZIEL/ffmpeg.gz"
for v in $VARIANTEN; do
    strip "$BAU/whisper-server-$v"
    gzip -9c "$BAU/whisper-server-$v" > "$ZIEL/whisper-server-$v.gz"
done
echo "$STAND" > "$ZIEL/STAND"
ls -l "$ZIEL"
echo "Whisper-Engine $ARCH gebaut ($STAND)"

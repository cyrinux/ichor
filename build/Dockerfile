# Android + gomobile toolchain. amd64 only: Android build-tools and the NDK
# are not published for linux/arm64 (runs under qemu binfmt on arm64 hosts).
FROM --platform=linux/amd64 docker.io/library/eclipse-temurin:17-jdk-jammy

ARG GO_VERSION=1.26.5
ARG GRADLE_VERSION=9.5.1
ARG CMDLINE_TOOLS=13114758
ARG ANDROID_PLATFORM=android-37
ARG BUILD_TOOLS=37.0.0
ARG NDK_VERSION=27.2.12479018

ENV ANDROID_HOME=/opt/android-sdk \
    ANDROID_NDK_HOME=/opt/android-sdk/ndk/${NDK_VERSION} \
    GOROOT=/usr/local/go \
    GOPATH=/go \
    GRADLE_HOME=/opt/gradle
ENV PATH=${GOROOT}/bin:${GOPATH}/bin:${GRADLE_HOME}/bin:${ANDROID_HOME}/cmdline-tools/latest/bin:${PATH}

RUN apt-get update \
 && apt-get install -y --no-install-recommends curl unzip git ca-certificates \
 && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xz

RUN curl -fsSL -o /tmp/gradle.zip "https://services.gradle.org/distributions/gradle-${GRADLE_VERSION}-bin.zip" \
 && unzip -q /tmp/gradle.zip -d /opt \
 && mv /opt/gradle-${GRADLE_VERSION} ${GRADLE_HOME} \
 && rm /tmp/gradle.zip

RUN curl -fsSL -o /tmp/tools.zip "https://dl.google.com/android/repository/commandlinetools-linux-${CMDLINE_TOOLS}_latest.zip" \
 && mkdir -p ${ANDROID_HOME}/cmdline-tools \
 && unzip -q /tmp/tools.zip -d ${ANDROID_HOME}/cmdline-tools \
 && mv ${ANDROID_HOME}/cmdline-tools/cmdline-tools ${ANDROID_HOME}/cmdline-tools/latest \
 && rm /tmp/tools.zip \
 && yes | sdkmanager --licenses >/dev/null \
 && sdkmanager --install "platform-tools" "platforms;${ANDROID_PLATFORM}" \
      "build-tools;${BUILD_TOOLS}" "ndk;${NDK_VERSION}" >/dev/null

WORKDIR /src

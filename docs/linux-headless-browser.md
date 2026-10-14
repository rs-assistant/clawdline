# When an agent on Linux cannot take a screenshot

An agent that checks a web page by eye — Playwright, Puppeteer, or Chromium's own
`--screenshot` — needs a browser and the shared libraries and fonts that browser loads. A
headless Linux server often has none of them, and the account the agent runs as often has no
`sudo`. This page takes an agent from the first error to a working screenshot, with or without
`sudo`. Every step is a command an agent can run and check by its output.

Measured on 2026-10-14 on Ubuntu 24.04 (noble), x86_64, as an ordinary user with no `sudo`, not in
the `docker` group, no browser installed and neither `/usr/share/fonts` nor `/etc/fonts` present.
Playwright was npm `playwright@1.49.1`, whose browser is
`~/.cache/ms-playwright/chromium_headless_shell-1148`. Package names below are noble's; §6 says
how to find them on another release.

## 1. Symptoms, in the order they appear

1. `chromium.launch()` fails with `Host system is missing dependencies to run browsers` and
   suggests `sudo npx playwright install-deps`.
2. `ldd` on the browser binary lists libraries as `not found`. On the measured machine the first
   pass named `libatk-1.0`, `libatk-bridge-2.0`, `libatspi`, `libXcomposite`, `libXdamage`,
   `libXfixes`, `libXrandr`, `libgbm` and `libasound`; once those were present, `libXi` and
   `libXrender` appeared. Expect more than one pass.
3. With every library present, `about:blank` screenshots, but the log says
   `Fontconfig error: Cannot load default config file` and any real page — including one with
   Chinese text — ends in `Page crashed` at `page.goto`. Adding `--disable-dev-shm-usage
   --disable-gpu --no-sandbox` does not help. The cause is the missing font configuration (§5).
4. With fonts and a `fonts.conf`, pages load and CJK text renders correctly.

## 2. Decide which path applies

Run all three and keep the answers.

```sh
# a. Can this account install system packages?
sudo -n true && echo SUDO=yes || echo SUDO=no

# b. Can this account run containers?
docker info >/dev/null 2>&1 && echo DOCKER=yes || echo DOCKER=no

# c. Which libraries does the browser lack? Install the browser first if it is absent:
#    npx playwright install chromium   (needs no sudo; it writes to ~/.cache/ms-playwright)
B=$(ls -d ~/.cache/ms-playwright/chromium_headless_shell-*/chrome-linux/headless_shell | tail -n 1)
ldd "$B" | grep "not found"
```

- `SUDO=yes` → §3.
- `SUDO=no`, `DOCKER=yes` → §3, the container route.
- Neither → §4, then §5.

`sudo -n` never waits for a password, so an agent's turn does not hang on a prompt. `docker info`
fails with `permission denied` on `/var/run/docker.sock` when the account is not in the `docker`
group. Do not ask the person for their password in a terminal an agent is driving; if `sudo` is
what you want, ask them to run the one command in §3 themselves.

## 3. With `sudo`, or with Docker

One command installs every library and a font set for the Playwright version in the project:

```sh
sudo npx playwright install-deps chromium
```

Run it from the project, so `npx` picks the project's Playwright version. The equivalent by hand
is `sudo apt-get install` with the packages listed in §4 plus `fonts-noto-cjk`. Then go to §7.

With Docker and no `sudo`, run the browser in Microsoft's image instead, matching the tag to the
project's Playwright version:

```sh
docker run --rm --ipc=host -v "$PWD":/work -w /work \
  mcr.microsoft.com/playwright:v1.49.1-noble npx playwright test
```

Adding the account to the `docker` group needs `sudo` once, and membership in that group is
equivalent to root on the machine; it is the person's decision, not the agent's.

## 4. Without `sudo`: unpack the libraries into your home directory

`apt-get download` fetches a package without installing it, and `dpkg -x` unpacks it anywhere;
neither needs root. The browser then finds the libraries through `LD_LIBRARY_PATH`.

```sh
L=~/.local/share/pw-libs
mkdir -p "$L/debs" && cd "$L/debs" || exit 1
apt-get download libatk1.0-0t64 libatk-bridge2.0-0t64 libatspi2.0-0t64 libxcomposite1 libxdamage1 \
  libxfixes3 libxrandr2 libgbm1 libasound2t64 libxi6 libxrender1 \
  fontconfig-config fonts-noto-cjk fonts-dejavu-core
for d in *.deb; do dpkg -x "$d" "$L/root"; done
```

The last three packages are the fonts and their configuration that §5 needs; they are downloaded
here so one pass fetches everything.

Check again with the unpacked libraries on the path:

```sh
LD_LIBRARY_PATH="$L/root/usr/lib/x86_64-linux-gnu" ldd "$B" | grep "not found"
```

No output means every library resolves. If a line remains, find its package (§6), add it to the
`apt-get download` line, unpack it, and check again. On a non-x86_64 machine the directory is the
one `dpkg-architecture -qDEB_HOST_MULTIARCH` prints, for example `aarch64-linux-gnu`.

## 5. The fonts: libraries alone still crash

Chromium asks fontconfig for fonts before it lays out any page with text. With no
`/etc/fonts/fonts.conf` and no fonts, fontconfig reports `Cannot load default config file` and the
renderer dies, which Playwright reports as `Page crashed`. `about:blank` has no text, so it still
screenshots, and that makes the libraries look like the whole fix when they are not.

Give fontconfig its own configuration pointing at the unpacked fonts. The heredoc delimiter is
unquoted on purpose, so `$L` is written out as the real path:

```sh
mkdir -p "$L/cache"
cat > "$L/fonts.conf" <<CONF
<?xml version="1.0"?><!DOCTYPE fontconfig SYSTEM "fonts.dtd">
<fontconfig>
  <dir>$L/root/usr/share/fonts</dir><cachedir>$L/cache</cachedir>
  <alias><family>sans-serif</family><prefer><family>Noto Sans CJK TC</family><family>DejaVu Sans</family></prefer></alias>
  <alias><family>serif</family><prefer><family>Noto Serif CJK TC</family><family>DejaVu Serif</family></prefer></alias>
  <alias><family>monospace</family><prefer><family>DejaVu Sans Mono</family></prefer></alias>
</fontconfig>
CONF
```

Every later run of Playwright or the browser binary carries both variables:

```sh
export LD_LIBRARY_PATH="$L/root/usr/lib/x86_64-linux-gnu" FONTCONFIG_FILE="$L/fonts.conf"
```

Put that line where the agent's commands will pick it up — the project's test script, or the
environment of the command that runs Playwright — rather than in a shell profile, so nothing else
on the machine loads these libraries by accident. Replace `TC` with `SC`, `JP` or `KR` in the
aliases when the pages are mostly in that language; `fonts-noto-cjk` contains all four.

## 6. Another release: finding the package for a missing `.so`

Package names change between releases. Ubuntu 24.04 and Debian 13 renamed many libraries with a
`t64` suffix during the 64-bit `time_t` transition (`libasound2` became `libasound2t64`,
`libatk1.0-0` became `libatk1.0-0t64`); older releases use the names without it. Do not copy the
list in §4 to another release; derive it from what `ldd` says is missing.

```sh
# The library name ldd printed, e.g. "libXi.so.6 => not found":
apt-file search libXi.so.6                 # exact: which package ships that file
apt-cache search --names-only '^libxi[0-9]'  # by name, when apt-file is not installed
apt-cache policy libxi6                    # confirms the name exists on this release
```

`apt-file` needs its own index (`apt-file update`), and installing it needs `sudo`; without it,
`apt-cache search` on the lower-cased library name usually finds the package, and the release's
package search site (packages.ubuntu.com or packages.debian.org, "search the contents of
packages") answers the exact question from a browser. `apt-get download` fails with
`Can't select candidate version` or `Unable to locate package` when a name is wrong for this
release, which is the signal to look the name up rather than to retry.

Fedora and other `dnf` systems have no `apt-get download`; `dnf download` with `rpm2cpio | cpio
-idm` does the same unpacking, and `dnf provides '*/libXi.so.6'` maps a file to its package.

## 7. Verify

Both of these must hold before the agent reports that screenshots work.

```sh
# 1. Every library resolves (no output):
ldd "$B" | grep "not found"

# 2. A page with text, including CJK, renders to an image:
T=$(mktemp -d)
printf '<html><body style="font-size:40px">Screenshot 截圖 テスト</body></html>' > "$T/check.html"
"$B" --headless --no-sandbox --screenshot="$T/check.png" --window-size=800,200 "file://$T/check.html"
ls -l "$T/check.png"
```

Run directly, the binary needs `--no-sandbox` on Ubuntu 23.10 and later: AppArmor forbids the
unprivileged user namespaces Chromium's sandbox uses
(`/proc/sys/kernel/apparmor_restrict_unprivileged_userns` is `1`), and without the flag it stops
with `No usable sandbox!` and dumps core. Playwright already passes `--no-sandbox` unless
`chromiumSandbox: true` is set, so this affects only the direct command. GPU warnings such as
`Failed to send GpuControl.CreateCommandBuffer` are harmless; the line that matters is
`... bytes written to file`.

On the §4 path, run both with `LD_LIBRARY_PATH` and `FONTCONFIG_FILE` exported. Open the image and
look at it: blank boxes in place of the Chinese and Japanese text mean fontconfig found no CJK font,
so check the `<dir>` in `fonts.conf` against where `fonts-noto-cjk` unpacked
(`ls "$L/root/usr/share/fonts"`). Then run the project's own Playwright command once, with a page
that has text, and confirm it does not end in `Page crashed`.

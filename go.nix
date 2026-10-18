# go.nix — this module's dependencies (FDR 0008); go.mod, gomod2nix.toml and
# the package graph are rendered or derived from it inside nix. Edit through
# the escape hatch (godyn-go) or by hand.
{
  flakeInputs = {
    "code.linenisgreat.com/madder/go" = {
      input = "madder";
    };
    "code.linenisgreat.com/purse-first/libs/go-mcp" = {
      input = "purse-first";
      subPath = "libs/go-mcp";
    };
    "code.linenisgreat.com/tommy" = {
      input = "tommy";
    };
  };
  go = "1.26.1";
  module = "code.linenisgreat.com/moxy";
  replace = { };
  require = {
    "code.linenisgreat.com/hyphence/go" = {
      go = "1.26";
      hash = "sha256-HMeJOBTmoABNHhQho2KUazvLAhDvdWCba8ZvMtKiEv8=";
      indirect = true;
      version = "v0.3.1-0.20260720154720-ea7f1e0933f9";
    };
    "code.linenisgreat.com/piggy/go" = {
      go = "1.26";
      hash = "sha256-HGekZfv1QS5BGlIlU+eJpTDQBd+7HqXNFJZWV/cpo7c=";
      version = "v0.0.0-20260720155209-77cfdea0031e";
    };
    "code.linenisgreat.com/purse-first/libs/dewey" = {
      go = "1.26";
      hash = "sha256-WaAb5xVdkek2jtlcD8y+99VuZyAoCVzezyIrs5X9Mbg=";
      version = "v0.5.0";
    };
    "code.linenisgreat.com/tap/go" = {
      go = "1.26";
      hash = "sha256-ifwq9+ER3gl317X0uizQkn2cYTtXiqWwPsFyfHZ91Bs=";
      indirect = true;
      version = "v0.2.0";
    };
    "filippo.io/age" = {
      go = "1.24.0";
      hash = "sha256-Qs/q3zQYV0PukABBPf/aU5V1oOhw95NG6K301VYJk8A=";
      indirect = true;
      version = "v1.3.1";
    };
    "filippo.io/hpke" = {
      go = "1.24.0";
      hash = "sha256-xKPT2hMzz/i3xdDK3NKn6FOmfQPxIMgxCOmm8U1heY4=";
      indirect = true;
      version = "v0.4.0";
    };
    "github.com/BurntSushi/toml" = {
      go = "1.18";
      hash = "sha256-ptdUJvuc21ixeLt+M5way/na3aCnCO4MYHWulWp8NEY=";
      version = "v1.6.0";
    };
    "github.com/DataDog/zstd" = {
      go = "1.14";
      hash = "sha256-GlSZOyix7Ct7tOKmSKpGckDjMhTtiYPBTpoWdwGLx5M=";
      indirect = true;
      version = "v1.5.7";
    };
    "github.com/atotto/clipboard" = {
      hash = "sha256-ZZ7U5X0gWOu8zcjZcWbcpzGOGdycwq0TjTFh/eZHjXk=";
      indirect = true;
      version = "v0.1.4";
    };
    "github.com/aws/aws-sdk-go-v2" = {
      go = "1.24";
      hash = "sha256-RaYrwS5mJVzZRcwXCq+qFzzmeXRER9FScXL6yc+Naag=";
      indirect = true;
      version = "v1.41.7";
    };
    "github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream" = {
      go = "1.24";
      hash = "sha256-nO21di7DmVoKQfXIqjxPBC5eObwNEuYiJh5LIAPoVO8=";
      indirect = true;
      version = "v1.7.10";
    };
    "github.com/aws/aws-sdk-go-v2/config" = {
      go = "1.24";
      hash = "sha256-zRUElXiDG4jD2t/NUS+VSKkaD+2aSA/kSfdxu0uczqw=";
      indirect = true;
      version = "v1.32.17";
    };
    "github.com/aws/aws-sdk-go-v2/credentials" = {
      go = "1.24";
      hash = "sha256-2qBR3nkluW+0cb9ASDz/6RPUj7PHydCp2RCkRQiJhpM=";
      indirect = true;
      version = "v1.19.16";
    };
    "github.com/aws/aws-sdk-go-v2/feature/ec2/imds" = {
      go = "1.24";
      hash = "sha256-87U454JA5SBrwkOaCzA3jSVkrhPfrrLgdyFRs5GjCRY=";
      indirect = true;
      version = "v1.18.23";
    };
    "github.com/aws/aws-sdk-go-v2/internal/configsources" = {
      go = "1.24";
      hash = "sha256-wSoEWAZEVfQJx64Cp413QPcD3ToRYokUc90eE8HdPFo=";
      indirect = true;
      version = "v1.4.23";
    };
    "github.com/aws/aws-sdk-go-v2/internal/endpoints/v2" = {
      go = "1.24";
      hash = "sha256-De8K7egEtlRvfeP6NESrayU/Y4tek1qz9rKMdXD/lBo=";
      indirect = true;
      version = "v2.7.23";
    };
    "github.com/aws/aws-sdk-go-v2/internal/v4a" = {
      go = "1.24";
      hash = "sha256-ZbVuj473RixTqKUHNb+w18p7tWTJQFnJ6AiWauOftvw=";
      indirect = true;
      version = "v1.4.24";
    };
    "github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding" = {
      go = "1.24";
      hash = "sha256-mV+ospdgmOIIoxBusWYJeg7vuSMs0GmoiA40j6OkfPY=";
      indirect = true;
      version = "v1.13.9";
    };
    "github.com/aws/aws-sdk-go-v2/service/internal/checksum" = {
      go = "1.24";
      hash = "sha256-m+k9Ujk0eMzQ+TnSbw/fxyJyR8gTJvBKX3BQaifBeRQ=";
      indirect = true;
      version = "v1.9.15";
    };
    "github.com/aws/aws-sdk-go-v2/service/internal/presigned-url" = {
      go = "1.24";
      hash = "sha256-hHNcwK3a+sxwL1GnjlXY1B8+PbsqX8iYIApixcBETgU=";
      indirect = true;
      version = "v1.13.23";
    };
    "github.com/aws/aws-sdk-go-v2/service/internal/s3shared" = {
      go = "1.24";
      hash = "sha256-rgNOU2gPoeUjSLLfTjo0l2qQ2KizyGXRKPw8M1PgbuU=";
      indirect = true;
      version = "v1.19.23";
    };
    "github.com/aws/aws-sdk-go-v2/service/s3" = {
      go = "1.24";
      hash = "sha256-wjlaGKTZ7B/6SDjeYR/2AUZX6MbL0yZQ1o3fr4jHRRQ=";
      indirect = true;
      version = "v1.101.0";
    };
    "github.com/aws/aws-sdk-go-v2/service/signin" = {
      go = "1.24";
      hash = "sha256-pqw4iyrwzcXrEdCYLNU/NP/4zXXDRijb+yG39R4q7mE=";
      indirect = true;
      version = "v1.0.11";
    };
    "github.com/aws/aws-sdk-go-v2/service/sso" = {
      go = "1.24";
      hash = "sha256-CS4NT3RJ+wiyBz7/YSE2BxZc6B4pKYJGCUEubgjhHSw=";
      indirect = true;
      version = "v1.30.17";
    };
    "github.com/aws/aws-sdk-go-v2/service/ssooidc" = {
      go = "1.24";
      hash = "sha256-McsKxup4dH4FEcPAU5DEfUwJLgaxuevvFzbAjsVW+v8=";
      indirect = true;
      version = "v1.35.21";
    };
    "github.com/aws/aws-sdk-go-v2/service/sts" = {
      go = "1.24";
      hash = "sha256-VTFdnpZSA648GU3cmJ9GCMq9h9DirZTn+znIM3eci94=";
      indirect = true;
      version = "v1.42.1";
    };
    "github.com/aws/smithy-go" = {
      go = "1.24";
      hash = "sha256-7Vq6Wnl5MrA2ITZGSNGuf8SNwWR+6usBRTgV7a6Cd7c=";
      indirect = true;
      version = "v1.25.1";
    };
    "github.com/aymanbagabas/go-osc52/v2" = {
      go = "1.16";
      hash = "sha256-6Bp0jBZ6npvsYcKZGHHIUSVSTAMEyieweAX2YAKDjjg=";
      indirect = true;
      version = "v2.0.1";
    };
    "github.com/brandondube/tai" = {
      go = "1.16";
      hash = "sha256-o32wvMW6waT/vJJkpjiCGxBNBUsqeAvnARhbsikRk50=";
      indirect = true;
      version = "v0.1.0";
    };
    "github.com/catppuccin/go" = {
      go = "1.19";
      hash = "sha256-otcMhI62ezoKGqzG7Owi/NROep7O0voJxp6bwXYg9+Q=";
      indirect = true;
      version = "v0.3.0";
    };
    "github.com/charmbracelet/bubbles" = {
      go = "1.24.2";
      hash = "sha256-Vz9QgctlzJqggPwfi48Lbn38ZJXu3Y71byp5uuuzUvU=";
      indirect = true;
      version = "v1.0.0";
    };
    "github.com/charmbracelet/bubbletea" = {
      go = "1.24.0";
      hash = "sha256-7wr85TLszu1CHNEMv+o4w+r24Z0xdzCgecPv+ZtRX/A=";
      indirect = true;
      version = "v1.3.10";
    };
    "github.com/charmbracelet/colorprofile" = {
      go = "1.24.2";
      hash = "sha256-d/NjM/ybG+bGRRRMMcjbPCFGFS5noZRMaL05Ix5r/II=";
      indirect = true;
      version = "v0.4.1";
    };
    "github.com/charmbracelet/huh" = {
      go = "1.23.0";
      hash = "sha256-vDqcsW9uBPDt0FaOA7Bij+Q9CkozggstOZ0r557TaC4=";
      version = "v1.0.0";
    };
    "github.com/charmbracelet/lipgloss" = {
      go = "1.18";
      hash = "sha256-RHsRT2EZ1nDOElxAK+6/DC9XAaGVjDTgPvRh3pyCfY4=";
      indirect = true;
      version = "v1.1.0";
    };
    "github.com/charmbracelet/x/ansi" = {
      go = "1.24.2";
      hash = "sha256-UToZIkqXl9MEppcRgbeBqaaMeAzRkGa0w3lVUs6sxWI=";
      indirect = true;
      version = "v0.11.6";
    };
    "github.com/charmbracelet/x/cellbuf" = {
      go = "1.24.2";
      hash = "sha256-0S60XaWhKZG+TB3Kqe1oMn2Okwdq53nym8XayVSHHiM=";
      indirect = true;
      version = "v0.0.15";
    };
    "github.com/charmbracelet/x/exp/strings" = {
      go = "1.19";
      hash = "sha256-NWe8LHXUtrrABWFhmAzLNYAZyJIwN3C/T2OdaInjl9E=";
      indirect = true;
      version = "v0.0.0-20240722160745-212f7b056ed0";
    };
    "github.com/charmbracelet/x/term" = {
      go = "1.24.0";
      hash = "sha256-KF7IU1Luxl/sZP6XjomWB2e3lxSUS4/5AahhapGir/4=";
      indirect = true;
      version = "v0.2.2";
    };
    "github.com/clipperhouse/displaywidth" = {
      go = "1.18";
      hash = "sha256-9CNyTZPSncKQ7Y0my9DR4WYXDjtDHYNL512D691WDAM=";
      indirect = true;
      version = "v0.9.0";
    };
    "github.com/clipperhouse/stringish" = {
      go = "1.18";
      hash = "sha256-Mp8M1CRbwr6dcJ4BD9tXD5I78ZgCFEm0GDxJv0GYReg=";
      indirect = true;
      version = "v0.1.1";
    };
    "github.com/clipperhouse/uax29/v2" = {
      go = "1.18";
      hash = "sha256-Men4JLhiuEtAx8ZSzId5ciRWhud68o3k/B48ppwyxkM=";
      indirect = true;
      version = "v2.5.0";
    };
    "github.com/danieljoos/wincred" = {
      go = "1.18";
      hash = "sha256-lDRkd1rU5EIyhzzCB0yIuQUpSdvMM2jjUsCg7rQaHMQ=";
      indirect = true;
      version = "v1.2.3";
    };
    "github.com/dsnet/compress" = {
      hash = "sha256-z7QnzNoFPeGd51fGDs+icELwL1PR7GEPYc4MNi0dxDY=";
      indirect = true;
      version = "v0.0.0-20171208185109-cc9eb1d7ad76";
    };
    "github.com/dustin/go-humanize" = {
      go = "1.16";
      hash = "sha256-yuvxYYngpfVkUg9yAmG99IUVmADTQA0tMbBXe0Fq0Mc=";
      indirect = true;
      version = "v1.0.1";
    };
    "github.com/erikgeiser/coninput" = {
      go = "1.16";
      hash = "sha256-OWSqN1+IoL73rWXWdbbcahZu8n2al90Y3eT5Z0vgHvU=";
      indirect = true;
      version = "v0.0.0-20211004153227-1c3628e74d0f";
    };
    "github.com/gabstv/go-bsdiff" = {
      hash = "sha256-ONsS1OjwBNQdPN3ZD2+vpW+IKonPrc1IwBJ1mMrxP9Y=";
      indirect = true;
      version = "v1.0.5";
    };
    "github.com/godbus/dbus/v5" = {
      go = "1.20";
      hash = "sha256-j+LicPlXcB1R3oVFPa8cd4yVJLpBx+ca5gXmHqOJSN8=";
      indirect = true;
      version = "v5.2.2";
    };
    "github.com/google/go-cmp" = {
      go = "1.21";
      hash = "sha256-JbxZFBFGCh/Rj5XZ1vG94V2x7c18L8XKB0N9ZD5F2rM=";
      indirect = true;
      version = "v0.7.0";
    };
    "github.com/google/uuid" = {
      hash = "sha256-VWl9sqUzdOuhW0KzQlv0gwwUQClYkmZwSydHG2sALYw=";
      version = "v1.6.0";
    };
    "github.com/kr/fs" = {
      hash = "sha256-+Cjz0rGmdNIV1QL4z8h7JAjHATa5pKndwSnD1M0J74c=";
      indirect = true;
      version = "v0.1.0";
    };
    "github.com/lucasb-eyer/go-colorful" = {
      go = "1.12";
      hash = "sha256-6BKrJsfmxie+YFAWzTYVPQfrwjQEXRo+J8LY+50C1BU=";
      indirect = true;
      version = "v1.3.0";
    };
    "github.com/mattn/go-isatty" = {
      go = "1.15";
      hash = "sha256-qhw9hWtU5wnyFyuMbKx+7RB8ckQaFQ8D+8GKPkN3HHQ=";
      indirect = true;
      version = "v0.0.20";
    };
    "github.com/mattn/go-localereader" = {
      hash = "sha256-JlWckeGaWG+bXK8l8WEdZqmSiTwCA8b1qbmBKa/Fj3E=";
      indirect = true;
      version = "v0.0.1";
    };
    "github.com/mattn/go-runewidth" = {
      go = "1.20";
      hash = "sha256-GpnbKplhX410Q/eIdknvWbYZgdav1keN+7wNUeOSMHE=";
      indirect = true;
      version = "v0.0.19";
    };
    "github.com/mitchellh/hashstructure/v2" = {
      go = "1.14";
      hash = "sha256-O4Yw4pPQECWe8DoVDIH2nUMN8Zl8waS7/O1sv18M2Xs=";
      indirect = true;
      version = "v2.0.2";
    };
    "github.com/muesli/ansi" = {
      go = "1.17";
      hash = "sha256-qRKn0Bh2yvP0QxeEMeZe11Vz0BPFIkVcleKsPeybKMs=";
      indirect = true;
      version = "v0.0.0-20230316100256-276c6243b2f6";
    };
    "github.com/muesli/cancelreader" = {
      go = "1.17";
      hash = "sha256-uEPpzwRJBJsQWBw6M71FDfgJuR7n55d/7IV8MO+rpwQ=";
      indirect = true;
      version = "v0.2.2";
    };
    "github.com/muesli/termenv" = {
      go = "1.17";
      hash = "sha256-hGo275DJlyLtcifSLpWnk8jardOksdeX9lH4lBeE3gI=";
      indirect = true;
      version = "v0.16.0";
    };
    "github.com/pkg/sftp" = {
      go = "1.23.0";
      hash = "sha256-YKjTWim2Qa6z3FA1dUFaKXSRxB4fsHDAG0GL17ZDE4U=";
      indirect = true;
      version = "v1.13.10";
    };
    "github.com/rivo/uniseg" = {
      go = "1.18";
      hash = "sha256-rDcdNYH6ZD8KouyyiZCUEy8JrjOQoAkxHBhugrfHjFo=";
      indirect = true;
      version = "v0.4.7";
    };
    "github.com/xo/terminfo" = {
      go = "1.19";
      hash = "sha256-GyCDxxMQhXA3Pi/TsWXpA8cX5akEoZV7CFx4RO3rARU=";
      indirect = true;
      version = "v0.0.0-20220910002029-abceb7e1c41e";
    };
    "github.com/zalando/go-keyring" = {
      go = "1.18";
      hash = "sha256-TnsYicaHuDBFITZGWiAW9L7OxvpQ4XbBFpLpOI7NteI=";
      version = "v0.2.8";
    };
    "golang.org/x/crypto" = {
      go = "1.25.0";
      hash = "sha256-/R74sc1mcOaOuBeXRQzrXrHAgA5VhNWc6SfQJaxb17U=";
      indirect = true;
      version = "v0.51.0";
    };
    "golang.org/x/exp" = {
      go = "1.25.0";
      hash = "sha256-JaDJGLIRoJjjvsg3dgfFuo7XApEJO2V4kUDmd58qTLI=";
      indirect = true;
      version = "v0.0.0-20260410095643-746e56fc9e2f";
    };
    "golang.org/x/sys" = {
      go = "1.25.0";
      hash = "sha256-JDlj+PKsG6I6kjv5JyOUNreY51u5An0oZ5OZMHZSk+A=";
      indirect = true;
      version = "v0.44.0";
    };
    "golang.org/x/term" = {
      go = "1.25.0";
      hash = "sha256-gFV1oGgs/vpRamvDWmu93voN57iyZMk/hh+oL8L9VrQ=";
      indirect = true;
      version = "v0.43.0";
    };
    "golang.org/x/text" = {
      go = "1.25.0";
      hash = "sha256-8XDOnlPIybcDRy89fkjG5VqtIt5Ku+LmaqYhgKl7i1E=";
      indirect = true;
      version = "v0.37.0";
    };
    "golang.org/x/xerrors" = {
      go = "1.18";
      hash = "sha256-bE7CcrnAvryNvM26ieJGXqbAtuLwHaGcmtVMsVnksqo=";
      indirect = true;
      version = "v0.0.0-20240903120638-7835f813f4da";
    };
    "mvdan.cc/sh/v3" = {
      go = "1.25.0";
      hash = "sha256-FM+2xpGELZLm4Gc09OE5Z3JeNLPMvzeb1wRcdAIeKy4=";
      indirect = true;
      version = "v3.13.1";
    };
  };
}

package buildinfo

import "encoding/base64"

// Values may be overridden at build time with -ldflags "-X ...".
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
	// ReleaseBaseURL and CertificateBase64 are set by the release build. Keeping
	// the certificate base64 encoded makes linker flags safe for every shell.
	ReleaseBaseURL    = "https://istok.s26.dev/releases/"
	CertificateBase64 = ""
)

type Info struct {
	Version           string
	Commit            string
	BuildDate         string
	ReleaseBaseURL    string
	CertificateBase64 string
}

func Certificate() ([]byte, error) {
	if CertificateBase64 == "" {
		return nil, ErrCertificateMissing
	}

	return base64.StdEncoding.DecodeString(CertificateBase64)
}

func Current() Info {
	return Info{Version: Version, Commit: Commit, BuildDate: BuildDate, ReleaseBaseURL: ReleaseBaseURL, CertificateBase64: CertificateBase64}
}

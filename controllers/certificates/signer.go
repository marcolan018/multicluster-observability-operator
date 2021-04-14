// Copyright (c) 2021 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package certificates

import (
	"errors"
	"time"

	"github.com/cloudflare/cfssl/config"
	"github.com/cloudflare/cfssl/signer"
	"github.com/cloudflare/cfssl/signer/local"
	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getClient() (client.Client, error) {
	config, err := clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		return nil, errors.New("failed to create the kube config")
	}
	c, err := client.New(config, client.Options{})
	if err != nil {
		return nil, errors.New("failed to create the kube client")
	}
	return c, nil
}

func sign(csr *certificatesv1.CertificateSigningRequest) []byte {
	c, err := getClient()
	if err != nil {
		log.Error(err, err.Error())
		return nil
	}
	_, caCert, caKey, err := getCA(c, false)
	if err != nil {
		return nil
	}

	var usages []string
	for _, usage := range csr.Spec.Usages {
		usages = append(usages, string(usage))
	}

	certExpiryDuration := 365 * 24 * time.Hour
	durationUntilExpiry := time.Until(caCert.NotAfter)
	if durationUntilExpiry <= 0 {
		log.Error(errors.New("signer has expired"), "the signer has expired: %v", caCert.NotAfter)
		return nil
	}
	if durationUntilExpiry < certExpiryDuration {
		certExpiryDuration = durationUntilExpiry
	}

	policy := &config.Signing{
		Default: &config.SigningProfile{
			Usage:        usages,
			Expiry:       certExpiryDuration,
			ExpiryString: certExpiryDuration.String(),
		},
	}
	cfs, err := local.NewSigner(caKey, caCert, signer.DefaultSigAlgo(caKey), policy)
	if err != nil {
		log.Error(err, "Failed to create new local signer")
		return nil
	}

	signedCert, err := cfs.Sign(signer.SignRequest{
		Request: string(csr.Spec.Request),
	})
	if err != nil {
		log.Error(err, "Failed to sign the CSR")
		return nil
	}
	return signedCert
}

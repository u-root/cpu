// Copyright 2018-2022 the u-root Authors. All rights reserved
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

var (
	privateKey = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEogIBAAKCAQEA4naTU4uxmqgpL+v6zIzANIpDeTmvaO+6t25HhlnfhlgDO2dZ
n4bh28HyPyQZC8b1xrEnGPL8+Wcd2hJwyY8oMwBwJmPahPcX7wlll6q5zqhK0tg0
CnCF8GoNrdBl0OnXduHQD6WxYGS7JIgSFKKwopgL8RCPg2ZY8rwlI+VwY7n6QjKl
8nSh6YjalkA9LUSNkf79rAXIhiXiWYJZzV+yUCYCVhb2tKWEDhtczflst1Y9NRDB
BnyVLPDGhi0oiO0eF/tltCOkOAx3iEsRR+HZ4E2E71cq6iUg/3KIEQioLk1TgpmG
RevIm5MGCtSJivErfklc569dg3MCvgFzMR2oowIDAQABAoIBAHLbfvdVl3uAJHuY
rPgHvwgmw/f86NlJFSMpfH9In9TMWL9NOKhvSagiotGhZk6R11+xw8mkm+eGhB5x
UeD4iYPsifT+mfrsM6hZ1LvqrBiDRIfRfft5fIUl1NA+LRWbNFuoRdVZzS+9hykN
FlZ++SVOBmh6ZL9ZLm3WPOQK30jEOze3zGFAQedZTeUI0R0YIULStLoToC8bjiAI
ym3tfltN+0rD8nh+A4+Cn2W00l52VqC+3GyymVLebNBYGmriuH4ru0aHARwZbxjo
WaRCIRa2tOmgcpRZdyJaxcHhr9hydAzcl2ToXTXS78gXDTBqUzQ8eVvjIx1m5pvz
gNWYzskCgYEA9s/JkmSqFLS8D1DUnhfHthyOA1HvDsz2uR15Wura7IOyZ4cEiq0b
BunrJA6gpNNCVXMzaeB4WtruRzx69jhmPutgVR9Si2r8uM66A5MU6AIFr9Pbmqo/
QR8/Vu9LXNEBJL1bTHXNe3n/ws2cwXQIPS3D12N2kHo7G5wLP07r/HUCgYEA6uTb
6M77ZSrqvpN1DBqB4jIHGtUBuH4+kBhcbu7HzpCZK2ORhd2X38gscNhEo9ofIEmu
mM9JUgnMtddq3z4574SuhhXrenOIGXb9JN5agEqF8U8LHZwkWv+CLEhGmqdFYsk2
VlQwzNNS6NPZ1Tuc2QmwzRIzXwL+xVacP1HpTbcCgYAZxUp70az8qn50bvE0bLE6
r7KYYCbA+d/NJmm0d49SYNHxA2UTAc4vo58czbYyX6iueW/l3z1R50g4AfWo3ey3
JyaQ3MtmqU4oEdXUZ7goHYXwfQOSG7KtHxEjB6trzpr69hahXi+NdAijk4qJnI77
rFqlk8oefdTMJjf6bUgwvQKBgHjPipd71WrcHu4z0zCNdZ4MEwFm6sKkE7NzBB9+
KkAAuPbK+C68oP9U6h6D7RHE/ttRaj5n5pMOPT6NdAcr7wpU2JpYLcvGHgrS2zIa
NrvjGG7bM6FgDIbNAXubFM04GQTM7miKVqsSSYM8ar40MeCjDk76/HbyiGygti4P
CAqTAoGAA6rHnhvE9nmrqI4vS3nwSYF5POhP3JjKQMGc5a3H96hf+eJjNihb0NR1
fxHFQ8imFzZ5OxTZOdAJwmbYO1iMS2oJ2GwAG5eI9w+xyyEu+QrCffHGHdFas1+C
uJ2GbxWXZEXGMHS79KrHXMnAFPZLYotgH1v+eTJ631lfZVFCOms=
-----END RSA PRIVATE KEY-----
`)

	publicKey = []byte(`ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDidpNTi7GaqCkv6/rMjMA0ikN5Oa9o77q3bkeGWd+GWAM7Z1mfhuHbwfI/JBkLxvXGsScY8vz5Zx3aEnDJjygzAHAmY9qE9xfvCWWXqrnOqErS2DQKcIXwag2t0GXQ6dd24dAPpbFgZLskiBIUorCimAvxEI+DZljyvCUj5XBjufpCMqXydKHpiNqWQD0tRI2R/v2sBciGJeJZglnNX7JQJgJWFva0pYQOG1zN+Wy3Vj01EMEGfJUs8MaGLSiI7R4X+2W0I6Q4DHeISxFH4dngTYTvVyrqJSD/cogRCKguTVOCmYZF68ibkwYK1ImK8St+SVznr12DcwK+AXMxHaij rminnich@xcpu`)
	hostKey   = []byte{}
	// Why %s and not %d?
	// https://github.com/kevinburke/ssh_config/issues/2
	// ssh_config does not do any of the % stuff yet.
	// So we have to rewrite this with the full path. Ouch.
	sshConfig = []byte(`
Host server
	HostName localhost
	Port 2222
	User root
	IdentityFile %s/server
`)
)

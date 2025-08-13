module golang.org/x/scratch/kusano/replacement

go 1.24.4

replace golang.org/x/crypto => ../../../this-does-not-exist

require golang.org/x/crypto v0.41.0 // indirect

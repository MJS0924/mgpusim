#!/bin/bash
# yann.lecun.com is no longer available; using Google storage mirror instead.
MIRROR="https://storage.googleapis.com/cvdf-datasets/mnist"

wget "$MIRROR/train-images-idx3-ubyte.gz" \
    --output-document train-images-idx3-ubyte.gz
wget "$MIRROR/train-labels-idx1-ubyte.gz" \
    --output-document train-labels-idx1-ubyte.gz
wget "$MIRROR/t10k-images-idx3-ubyte.gz" \
    --output-document t10k-images-idx3-ubyte.gz
wget "$MIRROR/t10k-labels-idx1-ubyte.gz" \
    --output-document t10k-labels-idx1-ubyte.gz

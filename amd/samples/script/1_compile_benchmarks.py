#!/usr/bin/python3

import subprocess
import os
import sys
import argparse
import re
import csv

path = "../"
benchmarks_path = path + "samples/"

class Test(object):

    def __init__(self, path):
        self.path = path
    
    def compile(self):
        fp.open(os.devnull, 'w')
        p = subprocess.Popen('go build', shell=True,
                             cwd=self.path, stdout=fp, stderr=fp)
        p.wait()
        if p.returncode == 0:
            print("Compiled " + self.path, 'green')
            return False
        else:
            print("Compile failed " + self.path, 'red')
            return True


def main():

    pagerank        =   Test(benchmarks_path + 'pagerank')
    kmeans          =   Test(benchmarks_path + 'kmeans')
    matrixtranspose =   Test(benchmarks_path + 'matrixtranspose')

    err = False

    err |= pagerank.compile()
    err |= kmeans.compile()
    err |= matrixtranspose.compile()

    print(err)

if __name__ == '__main__':
    main()

#!/usr/bin/python3

configs = ['baseline']

benchmarks = [
    'pagerank',
    'kmeans',
    'matrixtranspose',
]

for config in configs:
    for benchmark in benchmarks:
        print(config, benchmark)
        submit_file_name = config + "/" + benchmark + ".sh"
        submit_file = open(submit_file_name, "w")
        submit_file.write("#!/bin/bash\n")
        submit_file.write("cd samples\n")
        submit_file.write("cd " + benchmark + "\n")
        submit_file.write("./" + benchmark + " ")
        submit_file.write("-timing ")
        submit_file.write("-unified-gpus=1,2,3,4 ")
        submit_file.write("-use-unified-memory ")
        submit_file.write("-scheduling-alg=lasp ")

        if config == 'baseline':
            submit_file.write("-platform-type=baseline ")
            submit_file.write("-mem-allocator-type=lasp ")
            submit_file.write("-use-lasp-mem-alloc ")

        if benchmark == 'pagerank':
            submit_file.write("-sched-partition=Xdiv ")
        elif benchmark == 'kmeans':
            submit_file.write("-sched-partition=Xdiv ")
        elif benchmark == 'matrixtranspose':
            submit_file.write("-sched-partition=Xdiv ")

import sys

print(sys.argv[1])

f = open(sys.argv[1],"r")
lines = f.readlines()

participants = []
count = 0
command = 'eudico send --from t1d2xrzcslx7xlbbylc5c3d5lvandqw4iwl6epxba --method 2 --params-json "{\\"Miners\\":['
for line in lines:
    count += 1
    participant = line.split('/')[-1].rstrip()
    participants.append(participant)
    command = command + '\\"' + participant + '\\"'
    if count < len(lines):
        command = command + ','

command = command + ']}" t065 0'

print(command)

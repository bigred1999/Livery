# Fake Tuya bulb used by tuya_test.go to check Livery against the reference
# implementation. Needs TinyTuya from PyPI (pip install tinytuya).
# Usage: tuya_fakebulb.py <version> <port> <logfile>
import sys, os, json, socket, struct, hmac
from hashlib import sha256
from tinytuya.core.crypto_helper import AESCipher
from tinytuya.core.message_helper import pack_message, unpack_message, TuyaMessage, parse_header
from tinytuya.core import header as H

ver = sys.argv[1]; port = int(sys.argv[2]); logf = open(sys.argv[3], 'a')
V = float(ver)
REAL = b'0123456789abcdef'
DEVID = 'ebfakebulb1234567890ab'
dps = {"20": False, "21": "white", "22": 500, "23": 500, "24": "000003e803e8"}

def log(*a):
    print(*a, file=logf, flush=True)

BUF = {}
def recv_frame(c):
    d = BUF.get(c, b'')
    while len(d) < 18:
        x = c.recv(4096)
        if not x: return None
        d += x
    h = parse_header(d)
    need = (16 + h.length) if h.prefix == H.PREFIX_55AA_VALUE else (18 + h.length + 4)
    while len(d) < need:
        x = c.recv(4096)
        if not x: return None
        d += x
    BUF[c] = d[need:]
    return d[:need]

def serve(c):
    key = REAL
    seq = 100
    def reply(cmd, plain, retcode=0, use_header=True):
        nonlocal seq
        seq += 1
        if V >= 3.5:
            p = plain
            if use_header: p = (ver.encode() + b'\0'*12) + p
            m = TuyaMessage(seq, cmd, retcode, p, 0, True, H.PREFIX_6699_VALUE, True)
            return pack_message(m, hmac_key=key)
        if V >= 3.4:
            p = plain
            if use_header: p = (ver.encode() + b'\0'*12) + p
            enc = AESCipher(key).encrypt(p, False) if p else b''
            m = TuyaMessage(seq, cmd, 0, struct.pack('>I', retcode) + enc, 0, True, H.PREFIX_55AA_VALUE, False)
            return pack_message(m, hmac_key=key)
        enc = AESCipher(key).encrypt(plain, False) if plain else b''
        if use_header and plain: enc = (ver.encode() + b'\0'*12) + enc
        m = TuyaMessage(seq, cmd, 0, struct.pack('>I', retcode) + enc, 0, True, H.PREFIX_55AA_VALUE, False)
        return pack_message(m)
    client_nonce = None; dev_nonce = os.urandom(16)
    while True:
        f = recv_frame(c)
        if f is None: return
        hk = key if V >= 3.4 else None
        msg = unpack_message(f, hmac_key=hk, no_retcode=True)  # client frames carry no retcode
        log('rx cmd', msg.cmd, 'crc_good', msg.crc_good)
        if not msg.crc_good:
            log('BAD INTEGRITY'); return
        p = msg.payload
        if V >= 3.4 and V < 3.5 and p:
            p = AESCipher(key).decrypt(p, False, decode_text=False)
        if msg.cmd == 3:
            client_nonce = p[:16]
            body = dev_nonce + hmac.new(REAL, client_nonce, sha256).digest()
            c.sendall(reply(4, body, use_header=False)); continue
        if msg.cmd == 5:
            ok = hmac.new(REAL, dev_nonce, sha256).digest() == p[:32]
            log('finish hmac ok', ok)
            x = bytes(a ^ b for a, b in zip(client_nonce, dev_nonce))
            if V >= 3.5:
                key = AESCipher(REAL).encrypt(x, use_base64=False, pad=False, iv=client_nonce[:12])[12:28]
            else:
                key = AESCipher(REAL).encrypt(x, use_base64=False, pad=False)
            continue
        if V < 3.4:
            if p.startswith(ver.encode()): p = p[15:]
            p = AESCipher(key).decrypt(p, False, decode_text=False)
        else:
            if p.startswith(ver.encode()): p = p[15:]
        log('rx json', p)
        j = json.loads(p) if p else {}
        if msg.cmd in (0x0a, 0x10):
            c.sendall(reply(msg.cmd, json.dumps({"devId": DEVID, "dps": dps}).encode(), use_header=False))
        elif msg.cmd in (0x07, 0x0d):
            newd = j.get('dps') or j.get('data', {}).get('dps') or {}
            dps.update(newd)
            log('SET', json.dumps(newd, sort_keys=True))
            c.sendall(reply(msg.cmd, b''))
            c.sendall(reply(0x08, json.dumps({"dps": newd, "t": 1}).encode()))

s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(('127.0.0.1', port)); s.listen(1)
log('listening', ver, port)
while True:
    c, _ = s.accept()
    try: serve(c)
    except Exception as e:
        import traceback; log('ERR', traceback.format_exc())
    c.close()

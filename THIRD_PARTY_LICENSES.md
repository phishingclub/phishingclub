# Third-Party Licenses
This file includes licenses from projects that are not dependencies but is included in some modified way.

This project incorporates code from third-party sources under different licenses. While the overall project is licensed under AGPL-3.0, the following components retain their original licenses:

## EvilGinx2

**Source**: https://github.com/kgretzky/evilginx2
**License**: BSD-3-Clause
**Copyright**: Copyright (c) 2017-2023 Kuba Gretzky (@kgretzky)
**Usage**: Portions of the HTTP proxy functionality in `backend/proxy/proxy.go` are derived from EvilGinx2

### BSD-3-Clause License Text

```
Copyright (c) 2017-2023 Kuba Gretzky (@kgretzky)
All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

3. Neither the name of the copyright holder nor the names of its
   contributors may be used to endorse or promote products derived from
   this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

## Bettercap

**Source**: https://github.com/bettercap/bettercap
**License**: GPL-3.0
**Copyright**: Copyright (c) 2016-2023 Simone Margaritelli (@evilsocket)
**Usage**: Portions of the HTTP proxy functionality (via EvilGinx2) are derived from Bettercap

Note: EvilGinx2 itself incorporates and acknowledges code from the Bettercap project. Our usage maintains this attribution chain.

## IPVerse

**Source**: https://github.com/ipverse/rir-ip
**License**: CC0 1.0 Universal
**Copyright**:
**Usage**: Used for geo IP filtering

---

## License Compatibility

This project combines code under different licenses:

- **Overall Project**: AGPL-3.0 (see main LICENSE file)
- **BSD-3-Clause Components**: Compatible with AGPL-3.0, incorporated with proper attribution
- **GPL-3.0 Components**: Compatible with AGPL-3.0 through inheritance chain

All components are properly attributed and their usage complies with their respective license terms.

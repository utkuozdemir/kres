// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package conform

const configTemplate = `policies:
- type: commit
  spec:
    dco: true
    gpg: false
    spellcheck:
      locale: US
    maximumOfOneCommit: true
    header:
      length: 89
      imperative: true
      case: lower
      invalidLastCharacters: .
    body:
      required: true
    conventional:
      types: {{ .Types }}
      scopes: {{ .Scopes }}
- type: license
  spec:
    skipPaths:
      - .git/
    includeSuffixes:
      - .go
    excludeSuffixes:
      - .pb.go
    header: |
      // This Source Code Form is subject to the terms of the Mozilla Public
      // License, v. 2.0. If a copy of the MPL was not distributed with this
      // file, You can obtain one at http://mozilla.org/MPL/2.0/.
`

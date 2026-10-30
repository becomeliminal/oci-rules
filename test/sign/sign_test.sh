#!/bin/bash
# Signs a published image with a throwaway key, against a registry served
# here with no network, and checks what verification accepts and refuses.
set -euo pipefail

T=$(mktemp -d)
export HOME="$T" COSIGN_PASSWORD=""
"$DATA_REGISTRY" "$T/addr" 2> "$T/registry.log" &
REGISTRY_PID=$!
trap 'kill $REGISTRY_PID 2>/dev/null || true' EXIT
for _ in $(seq 50); do [ -s "$T/addr" ] && break; sleep 0.1; done
HOST=$(cat "$T/addr")

# Publish the image and an unsigned one, as oci-tool push does in a deploy.
publish() {
  local manifest=$1 config=$2 digest=$3 blobs=$4
  echo "$HOST/team/app@$digest" > "$T/fqn"
  echo "$HOST/team/app:keep-sha256-${digest#sha256:}" > "$T/retain"
  "$DATA_OCI_TOOL" push --manifest "$manifest" --config "$config" --fqn "$T/fqn" --retain "$T/retain" \
    $(for b in $blobs; do printf -- '--blob %s ' "$b"; done) > /dev/null
}
DIGEST=$(cat "$DATA_DIGEST")
publish "$DATA_MANIFEST" "$DATA_CONFIG" "$DIGEST" "$DATA_BLOBS"
cp "$T/fqn" "$T/signed.fqn"
OTHER=$(cat "$DATA_OTHER_DIGEST")
publish "$DATA_OTHER_MANIFEST" "$DATA_OTHER_CONFIG" "$OTHER" "$DATA_OTHER_BLOBS"

# A key pair, a signing config with no Sigstore services, and a trusted root
# with no Sigstore material: nothing here reaches a transparency log, a
# certificate authority or Sigstore's TUF mirror (which cosign otherwise
# fetches, waiting out a dropped connection for half a minute a call).
echo "== keys"
"$DATA_COSIGN" generate-key-pair --output-key-prefix "$T/cosign" > /dev/null
"$DATA_COSIGN" generate-key-pair --output-key-prefix "$T/other" > /dev/null
"$DATA_COSIGN" signing-config create --no-default-fulcio --no-default-rekor --no-default-tsa --no-default-oidc --out "$T/sc.json" > /dev/null
echo '{"mediaType":"application/vnd.dev.sigstore.trustedroot+json;version=0.1"}' > "$T/trusted-root.json"
INSECURE="--cosign-flag=--allow-insecure-registry --cosign-flag=--allow-http-registry --cosign-flag=--trusted-root=$T/trusted-root.json"

echo "== sign"
"$DATA_OCI_TOOL" sign --ref "$T/signed.fqn" --layout "$DATA_LAYOUT" --meta "$DATA_META" \
  --target //test/image:image \
  --cosign "$DATA_COSIGN" --syft "$DATA_SYFT" --key "$T/cosign.key" \
  --cosign-flag=--signing-config="$T/sc.json" $INSECURE \
  --build-type https://example.com/oci-rules/image/v1 --source-uri git+https://example.com/repo \
  --commit 0123456789abcdef0123456789abcdef01234567 --builder-id https://example.com/ci --invocation-id https://example.com/ci/runs/1

verify() {
  "$DATA_OCI_TOOL" verify --ref "$1" --public-key "$2" --cosign "$DATA_COSIGN" \
    --cosign-flag=--insecure-ignore-tlog $INSECURE > /dev/null 2>&1
}

# The signature, the SBOM and the provenance verify against the key.
verify "$T/signed.fqn" "$T/cosign.pub" || { echo "the signed image did not verify"; exit 1; }

# Signing changed nothing about the image: its retention tag still names
# the digest that was published.
TAGGED=$(HOME="$T" "$DATA_REGCTL" image digest --host "reg=$HOST,tls=disabled" "$HOST/team/app:keep-sha256-${DIGEST#sha256:}")
[ "$TAGGED" = "$DIGEST" ] || { echo "the retention tag now names $TAGGED, not $DIGEST"; exit 1; }

# Another key's public half, and an image nobody signed, are refused.
if verify "$T/signed.fqn" "$T/other.pub"; then echo "verified against the wrong key"; exit 1; fi
if verify "$T/fqn" "$T/cosign.pub"; then echo "an unsigned image verified"; exit 1; fi

# An image carrying only attestations signed by the key counts as signed:
# under cosign v3 an attestation is the key's signature over the image's
# digest too, so `cosign verify` accepts it. Pinned here so that a cosign
# upgrade changing it is noticed. What still matters is that the key vouched
# for the digest, and that both attestations are there.
echo '{}' > "$T/empty.json"
for type in spdxjson slsaprovenance1; do
  "$DATA_COSIGN" attest --yes --key "$T/cosign.key" --type "$type" --predicate "$T/empty.json" \
    --signing-config "$T/sc.json" --trusted-root "$T/trusted-root.json" \
    --allow-insecure-registry --allow-http-registry "$(cat "$T/fqn")" > /dev/null 2>&1
done
verify "$T/fqn" "$T/cosign.pub" || { echo "cosign no longer counts an attested image as signed"; exit 1; }
# An index: each child is signed with its SBOM and provenance, in the
# index's repository, then the index; the whole verifies, and not under
# another key.
echo "$HOST/team/multi@$(cat "$DATA_INDEX_DIGEST")" > "$T/index.fqn"
echo "$HOST/team/multi:keep-sha256-$(sed 's/^sha256://' "$DATA_INDEX_DIGEST")" > "$T/index.retain"
"$DATA_OCI_TOOL" push-index --index "$DATA_INDEX" --fqn "$T/index.fqn" --retain "$T/index.retain" \
  --child-manifest "$DATA_AMD64_MANIFEST" --child-config "$DATA_AMD64_CONFIG" \
  --child-manifest "$DATA_ARM64_MANIFEST" --child-config "$DATA_ARM64_CONFIG" \
  $(for b in $DATA_INDEX_BLOBS; do printf -- '--blob %s ' "$b"; done) > /dev/null
"$DATA_OCI_TOOL" sign --index "$DATA_INDEX" --index-ref "$T/index.fqn" \
  --child-meta "$DATA_AMD64_META" --child-layout "$DATA_AMD64_LAYOUT" \
  --child-meta "$DATA_ARM64_META" --child-layout "$DATA_ARM64_LAYOUT" \
  --target //test/index:index \
  --cosign "$DATA_COSIGN" --syft "$DATA_SYFT" --key "$T/cosign.key" \
  --cosign-flag=--signing-config="$T/sc.json" $INSECURE \
  --build-type https://example.com/oci-rules/image/v1 --source-uri git+https://example.com/repo \
  --commit 0123456789abcdef0123456789abcdef01234567 --builder-id https://example.com/ci --invocation-id https://example.com/ci/runs/1 > /dev/null
verify_index() {
  "$DATA_OCI_TOOL" verify --index "$DATA_INDEX" --index-ref "$T/index.fqn" --child-meta "$DATA_AMD64_META" --child-meta "$DATA_ARM64_META" \
    --public-key "$1" --cosign "$DATA_COSIGN" --cosign-flag=--insecure-ignore-tlog $INSECURE > /dev/null 2>&1
}
verify_index "$T/cosign.pub" || { echo "the signed index did not verify"; exit 1; }
if verify_index "$T/other.pub"; then echo "the index verified against the wrong key"; exit 1; fi

echo "signed, verified, and refused what it must"

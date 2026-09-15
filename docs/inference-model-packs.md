# Inference model acquisition

Model weights are not part of the appliance delivery packs. Packaging every
model family, size, and quantization would make releases impractically large and
would couple platform upgrades to model choice.

The normal path is an administrator-directed connected window after install:

1. Connect the appliance to a network that can reach the model source.
2. Open **Admin → AI Services**.
3. Enter an engine-supported model reference and start the import.
4. Wait until the model appears in the installed-model list and validate it
   through `/ai/v1/models` and an inference request.
5. Disconnect the appliance. Installed models remain on persistent storage and
   inference continues without internet access.

The lifecycle API is described in
[inference-capability-phasing.md](inference-capability-phasing.md). It serializes
model-changing operations in the initial implementation. Downloads are never
started during appliance installation, startup, or in the background.

## Validation boundaries

The API accepts model references, not arbitrary URLs or filesystem paths. The
common runtime manager invokes the selected engine adapter and validates the
download. If the accelerated runtime accepts an expected digest, the manager
verifies it before publishing the model as installed. Ollama pull cannot
return an independently verifiable aggregate digest to the appliance, so the API
rejects an expected digest for that path rather than reporting false assurance.

Compatibility checks are capability-based: architecture, available CPU/CUDA
mode, storage, memory, and runtime-reported model requirements. They must not be
a hardcoded list of machine vendors, product names, or individual models.
Automatic best-mode selection is the installation default: verified CUDA is
preferred when the package and pod runtime support it, with CPU as fallback.
The admin UI does not expose a manual mode selector.

## Offline transfer fallback

`zonctl models-import` remains a low-level offline transfer mechanism for a
separately prepared and signed `appliance.modelpack/v1`. It verifies the pack and
copies its blobs into `/data/zon/inference/models`. It is not the primary model
catalog and a blob copy alone does not guarantee registration with every engine;
engine-specific registration must complete before the admin API lists the model.

This fallback exists for sites that cannot temporarily connect the appliance.
It does not justify putting model weights in the platform bundle or adding model
names to package metadata.

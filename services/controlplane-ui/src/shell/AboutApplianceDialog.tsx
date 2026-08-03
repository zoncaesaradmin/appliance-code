import React, { useEffect, useState } from "react";
import { BrandMark } from "../components";
import { client } from "../lib/api";
import { displayProductVersion } from "../productVersion";

export function AboutApplianceDialog(props: {
  open: boolean;
  onClose: () => void;
}): React.JSX.Element | null {
  const [productVersion, setProductVersion] = useState("");
  const year = new Date().getFullYear();

  useEffect(() => {
    if (!props.open) {
      return;
    }
    let cancelled = false;
    client
      .getVersion()
      .then((info) => {
        if (!cancelled) {
          setProductVersion(displayProductVersion(info.version));
        }
      })
      .catch(() => {
        if (!cancelled) {
          setProductVersion("");
        }
      });
    return () => {
      cancelled = true;
    };
  }, [props.open]);

  useEffect(() => {
    if (!props.open) {
      return;
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        props.onClose();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [props.open, props.onClose]);

  if (!props.open) {
    return null;
  }

  const versionLabel = productVersion ? `Version ${productVersion}` : "Version unavailable";

  return (
    <div
      className="fixed inset-0 z-[120] flex items-center justify-center bg-slate-950/45 p-4"
      role="presentation"
      onClick={props.onClose}
    >
      <div
        className="flex w-full max-w-md flex-col items-center rounded-[1.75rem] border border-slate-200 bg-white px-8 py-10 text-center shadow-2xl shadow-slate-900/25"
        role="dialog"
        aria-modal="true"
        aria-labelledby="about-appliance-title"
        onClick={(event) => event.stopPropagation()}
      >
        <BrandMark size="lg" />
        <h2 id="about-appliance-title" className="mt-5 m-0 text-2xl font-bold tracking-tight text-slate-950">
          Zon Appliance
        </h2>
        <p className="mt-2 m-0 text-sm font-semibold text-slate-500">{versionLabel}</p>
        <p className="mt-6 m-0 max-w-sm text-xs leading-5 text-slate-500">
          © {year} Zon Systems. All rights reserved.
        </p>
        <button className="button button--primary mt-8 min-w-36" type="button" onClick={props.onClose}>
          Close
        </button>
      </div>
    </div>
  );
}

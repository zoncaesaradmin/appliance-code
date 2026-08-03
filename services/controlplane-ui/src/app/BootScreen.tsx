import type React from "react";
import { BrandMark } from "../components";

export function BootScreen(props: { message: string; error?: boolean }): React.JSX.Element {
  return (
    <div className="boot-screen">
      <div className="boot-card">
        <BrandMark />
        <h1>{props.error ? "UI unavailable" : "Preparing appliance UI"}</h1>
        <p>{props.message}</p>
      </div>
    </div>
  );
}

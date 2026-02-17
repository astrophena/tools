import React from "npm:react";

export function ChartsGrid(props: {
  children: React.ReactNode;
}) {
  return (
    <div className="chart-matrix">
      {props.children}
    </div>
  );
}

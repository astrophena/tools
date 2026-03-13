import React from "react";

export function IndicatorGridSkeleton() {
  return (
    <div className="indicator-grid">
      {Array.from({ length: 6 }).map((_, i) => (
        <article
          key={i}
          className={`indicator skeleton ${
            i === 0 ? "indicator-primary" : i === 2 ? "indicator-danger" : ""
          }`}
        >
          <div className="skeleton-text short o-50"></div>
          <div className="skeleton-text h-1-5em mt-0-3rem w-80"></div>
          <div className="skeleton-text o-70"></div>
          <div className="skeleton-text short o-50 mt-0-45rem"></div>
        </article>
      ))}
    </div>
  );
}

export function ChartSkeleton() {
  return (
    <article className="chart-card skeleton">
      <div className="skeleton-text short h-1-2em o-70"></div>
      <div className="skeleton-text o-50"></div>
      <div className="skeleton-text h-180px mt-1rem o-10"></div>
    </article>
  );
}

export function GlobalSkeleton() {
  return (
    <div className="column" style={{ width: "100%" }}>
      <article className="panel skeleton" style={{ minHeight: "200px" }}>
        <div className="skeleton-text short h-1-5em o-70"></div>
        <div className="skeleton-text o-50 mt-0-45rem"></div>
        <div className="skeleton-text h-180px mt-1rem o-10"></div>
      </article>
      <article className="panel skeleton" style={{ minHeight: "200px" }}>
        <div className="skeleton-text short h-1-5em o-70"></div>
        <div className="skeleton-text o-50 mt-0-45rem"></div>
        <div className="skeleton-text h-180px mt-1rem o-10"></div>
      </article>
    </div>
  );
}

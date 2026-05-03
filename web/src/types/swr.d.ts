import "swr";

declare module "swr" {
  interface SWRConfiguration {
    refreshIntervalWhenHidden?: number;
    disableActivityTracking?: boolean;
  }
}

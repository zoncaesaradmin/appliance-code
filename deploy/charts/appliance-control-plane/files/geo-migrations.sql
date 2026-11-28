CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE TABLE IF NOT EXISTS public.zone_schema_migrations (
  version text PRIMARY KEY,
  applied_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS public.zones (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 120),
  description text NOT NULL DEFAULT '',
  boundary geometry(MultiPolygon, 4326) NOT NULL CHECK (NOT ST_IsEmpty(boundary) AND ST_IsValid(boundary)),
  revision integer NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS zones_boundary_gix ON public.zones USING gist(boundary);
CREATE INDEX IF NOT EXISTS zones_geography_gix ON public.zones USING gist((boundary::geography));
CREATE SCHEMA IF NOT EXISTS api;
CREATE OR REPLACE VIEW api.zones AS
SELECT id, name, description, revision, created_at, updated_at,
  ST_AsGeoJSON(boundary)::json AS geometry,
  ST_AsGeoJSON(ST_Centroid(boundary))::json AS centroid,
  ST_AsGeoJSON(ST_PointOnSurface(boundary))::json AS representative_point,
  ST_Area(boundary::geography) AS area_sq_m,
  ST_Perimeter(boundary::geography) AS perimeter_m
FROM public.zones;
CREATE OR REPLACE FUNCTION api.save_zone(p_name text, p_description text, p_geometry jsonb, p_id uuid DEFAULT NULL, p_revision integer DEFAULT NULL)
RETURNS uuid LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, public AS $$
DECLARE shape geometry; result uuid;
BEGIN
  IF p_geometry->>'type' NOT IN ('Polygon', 'MultiPolygon') THEN RAISE EXCEPTION 'Boundary must be a Polygon or MultiPolygon'; END IF;
  shape := ST_Multi(ST_SetSRID(ST_GeomFromGeoJSON(p_geometry),4326));
  IF ST_NDims(shape) <> 2 OR ST_IsEmpty(shape) OR NOT ST_IsValid(shape) OR ST_NPoints(shape) > 10000 THEN RAISE EXCEPTION 'Invalid boundary: %', ST_IsValidReason(shape); END IF;
  IF ST_XMin(shape) < -180 OR ST_XMax(shape) > 180 OR ST_YMin(shape) < -90 OR ST_YMax(shape) > 90 THEN RAISE EXCEPTION 'Coordinates are out of range'; END IF;
  IF p_id IS NULL THEN
    INSERT INTO public.zones(name, description, boundary) VALUES(p_name, coalesce(p_description,''), shape) RETURNING id INTO result;
  ELSE
    UPDATE public.zones SET name=p_name, description=coalesce(p_description,''), boundary=shape, revision=revision+1, updated_at=now() WHERE id=p_id AND revision=p_revision RETURNING id INTO result;
    IF result IS NULL THEN RAISE EXCEPTION 'Zone not found or revision conflict'; END IF;
  END IF;
  RETURN result;
END $$;
CREATE OR REPLACE FUNCTION api.export_zones() RETURNS jsonb LANGUAGE sql STABLE AS $$
  SELECT jsonb_build_object('type','FeatureCollection','features',COALESCE(jsonb_agg(jsonb_build_object('type','Feature','id',z.id,'geometry',ST_AsGeoJSON(z.boundary)::jsonb,'properties',jsonb_build_object('name',z.name,'description',z.description,'revision',z.revision,'area_sq_m',ST_Area(z.boundary::geography),'perimeter_m',ST_Perimeter(z.boundary::geography)) ) ORDER BY z.name,z.id),'[]'::jsonb)) FROM public.zones z;
$$;
GRANT USAGE ON SCHEMA api TO geo;
GRANT SELECT ON api.zones, public.zones TO geo;
GRANT EXECUTE ON FUNCTION api.save_zone(text,text,jsonb,uuid,integer), api.export_zones() TO geo;
INSERT INTO public.zone_schema_migrations(version) VALUES ('001_init.sql'), ('002_export.sql'), ('003_access.sql'), ('004_migration_history.sql') ON CONFLICT DO NOTHING;

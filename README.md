# imgredactor

Entfernt personenbezogene Daten aus Screenshots (PNG, JPG) – schnell, mit
Schwärz-Stift, Weichzeichner und Radierer. Gebaut mit
[gift](https://github.com/worldiety/gift).

```sh
go run . [ordner]
```

- **Ordner öffnen**: oben links, oder als Argument. Unten erscheint die
  Filmleiste mit allen Bildern, darüber groß das aktuelle Bild.
- **Werkzeuge**: Schwärzen (deckend schwarz), Radierer (stellt das Original
  wieder her), Weichzeichnen (starker Blur). Die Pinselgröße stellt der Slider
  ein; der Kreis unter dem Mauszeiger zeigt sie.
- **Speichern** geschieht automatisch, sobald ein anderes Bild gewählt, der
  Ordner geschlossen oder das Fenster geschlossen wird – und nur, wenn sich
  etwas geändert hat. Die Datei wird atomar ersetzt; das Format bleibt.
- **Solange das Bild offen ist**, liegen alle Änderungen nur im Speicher:
  *Rückgängig* nimmt den letzten Strich zurück, *Zurücksetzen* alle.

| Taste | Wirkung |
| --- | --- |
| ← / → | vorheriges / nächstes Bild |
| 1 / 2 / 3 (S / R / W) | Schwärzen / Radierer / Weichzeichnen |
| [ / ] | Pinsel kleiner / größer |
| ⌘Z / Strg+Z | letzten Strich rückgängig |
| ⇧ + Klick | gerade Linie vom Ende des letzten Strichs |

Hinweise: Gespeicherte JPEGs verlieren ihre EXIF-Daten (inklusive Ort), und
für die Thumbnails gibt es bewusst keinen Platten-Cache, damit keine Kopien der
ungeschwärzten Bilder liegen bleiben. Schwärzen ist sicherer als
Weichzeichnen.

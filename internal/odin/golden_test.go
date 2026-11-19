package odin

import (
	"strings"
	"testing"
)

const goldenWant = "\n" + `############################################################
## Sample.asset  (1787 bytes)
############################################################
_data { <Asset+Data>
  mCost = 1  (int)
  mScoreDic { <Dict>
    [array len=2]
      · { 
        · = 1  (int)
        · = 10  (int)
      }
      · { 
        · = 2  (int)
        · = 20  (int)
      }
    [/array]
  }
  mRateList { <List>
    [array len=2]
      · { <Rate>
        mScore = 10  (int)
        mRate = 0.35  (float)
      }
      · { <Rate>
        mScore = 20  (int)
        mRate = 151.1364  (float)
      }
    [/array]
  }
  floats { <Single[]>
    = [1.5, -0.25]  (float[] n=2)
  }
  doubles { <Double[]>
    = [2.125]  (double[] n=1)
  }
  ints { <Int32[]>
    = [7, -1]  (int[] n=2)
  }
  longs { <Int64[]>
    = [1099511627776]  (long[] n=1)
  }
  bytes { <Byte[]>
    = [222, 173]  (b1[] n=2)
  }
  shorts { <#id99>
    = [-2, 5]  (b2[] n=2)
  }
  raw8 { 
    = [3]  (b8[] n=1)
  }
  raw4 { 
    = [9]  (b4[] n=1)
  }
  raw3 { 
    = [1]  (b3[] n=1)
  }
  sb = -1  (sbyte)
  by = 200  (byte)
  sh = -3  (short)
  us = 65535  (ushort)
  ui = 4000000000  (uint)
  lo = -5  (long)
  ul = 18446744073709551615  (ulong)
  do = 0.123457  (double)
  ch = Z  (char)
  st = hello  (string)
  bo = True  (bool)
  nu = null
  · { 
    · = -128  (sbyte)
    · = 7  (byte)
    · = 3  (short)
    · = 4  (ushort)
    · = 5  (uint)
    · = 6  (long)
    · = 7  (ulong)
    · = 8.5  (float)
    · = 9.5  (double)
    · = q  (char)
    · = u  (string)
    · = False  (bool)
  }
}

############################################################
## small.asset  (28 bytes)
############################################################
root { 
  x = 7  (int)
}
`

func TestGoldenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	writeOdinAsset(t, dir, "Sample", "0123456789abcdef0123456789abcdef", sampleBlob())
	writeAssetWithBytes(t, dir, "small.asset", buildBlob())
	got := DecodeDir(dir, []string{"Sample.asset", "small.asset"})

	want := goldenWant
	gotN := normalizeNL(got)
	if gotN != want {
		// Emit first divergent line for debugging.
		gl := splitLines(gotN)
		wl := splitLines(want)
		n := min(len(gl), len(wl))
		for i := range n {
			if gl[i] != wl[i] {
				t.Fatalf("line %d differs:\n got=%q\nwant=%q", i+1, gl[i], wl[i])
			}
		}
		t.Fatalf("line count differs: got %d, want %d", len(gl), len(wl))
	}
}

func normalizeNL(s string) string  { return strings.ReplaceAll(s, "\r\n", "\n") }
func splitLines(s string) []string { return strings.Split(s, "\n") }
